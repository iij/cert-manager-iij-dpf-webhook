// SPDX-License-Identifier: Apache-2.0

package solver

import (
	"errors"
	"fmt"
	"time"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
)

// 配備単位の設定を与える環境変数 (data-model.md の DeploymentOptions)。認証情報は含まない。
const (
	EnvLockOwner         = "DPF_WEBHOOK_LOCK_OWNER"
	EnvExpireGrace       = "DPF_WEBHOOK_EXPIRE_GRACE"
	EnvExpireThreshold   = "DPF_WEBHOOK_EXPIRE_THRESHOLD"
	EnvOperationDeadline = "DPF_WEBHOOK_OPERATION_DEADLINE"
	EnvLockTTL           = "DPF_WEBHOOK_LOCK_TTL"
	EnvLockWait          = "DPF_WEBHOOK_LOCK_WAIT"
	EnvPodNamespace      = "POD_NAMESPACE"
)

// maxLockOwnerLen はロックオーナーの上限 (dpf-go v0.6.0 の保持者の上限。FR-048)。
const maxLockOwnerLen = 32

// ErrInvalidOptions は配備単位の設定が不足または不正な場合に返される。起動を中止する (FR-050)。
var ErrInvalidOptions = errors.New("invalid deployment options")

// Options は配備単位の設定である。
type Options struct {
	// LockOwner はロックオーナーである。必須。既定値を推測しない (FR-050)。
	LockOwner string
	// Grace は期限 (回収の期限) の猶予である。
	Grace time.Duration
	// Threshold は期限を更新する閾値である。猶予より短い。
	Threshold time.Duration
	// Deadline は操作期限である。60 秒より長くする (FR-057)。
	Deadline time.Duration
	// LockTTL はゾーンロックの保持期間である。
	LockTTL time.Duration
	// LockWait はゾーンロックの取得を再試行する間隔である。
	LockWait time.Duration
	// PodNamespace は本 Pod の namespace である。ClusterIssuer の既定の取得元になる。
	PodNamespace string
}

// DefaultOptions は既定値を返す。LockOwner と PodNamespace は空である。
func DefaultOptions() Options {
	return Options{
		Grace:     time.Hour,
		Threshold: 30 * time.Minute,
		Deadline:  5 * time.Minute,
		LockTTL:   dpf.DefaultLockTTL,
		LockWait:  dpf.DefaultLockWait,
	}
}

// OptionsFromEnv は環境変数から配備単位の設定を読み、検証する。
func OptionsFromEnv(getenv func(string) string) (Options, error) {
	o := DefaultOptions()
	o.LockOwner = getenv(EnvLockOwner)
	o.PodNamespace = getenv(EnvPodNamespace)

	thresholdSet := false
	for _, d := range []struct {
		env string
		dst *time.Duration
	}{
		{EnvExpireGrace, &o.Grace},
		{EnvExpireThreshold, &o.Threshold},
		{EnvOperationDeadline, &o.Deadline},
		{EnvLockTTL, &o.LockTTL},
		{EnvLockWait, &o.LockWait},
	} {
		v := getenv(d.env)
		if v == "" {
			continue
		}
		parsed, err := time.ParseDuration(v)
		if err != nil {
			return Options{}, fmt.Errorf("%w: %s: %v", ErrInvalidOptions, d.env, err) //nolint:errorlint // 解析の詳細はメッセージとしてのみ残す
		}
		*d.dst = parsed
		if d.env == EnvExpireThreshold {
			thresholdSet = true
		}
	}
	if !thresholdSet {
		o.Threshold = o.Grace / 2
	}
	if err := o.Validate(); err != nil {
		return Options{}, err
	}
	return o, nil
}

// Validate は設定を検証する。
func (o Options) Validate() error {
	if err := validateLockOwner(o.LockOwner); err != nil {
		return err
	}
	if o.PodNamespace == "" {
		return fmt.Errorf("%w: %s is required", ErrInvalidOptions, EnvPodNamespace)
	}
	for name, d := range map[string]time.Duration{
		EnvExpireGrace: o.Grace, EnvExpireThreshold: o.Threshold, EnvOperationDeadline: o.Deadline,
		EnvLockTTL: o.LockTTL, EnvLockWait: o.LockWait,
	} {
		if d <= 0 {
			return fmt.Errorf("%w: %s must be positive", ErrInvalidOptions, name)
		}
	}
	if o.Threshold >= o.Grace {
		return fmt.Errorf("%w: %s must be shorter than %s", ErrInvalidOptions, EnvExpireThreshold, EnvExpireGrace)
	}
	return nil
}

// validateLockOwner はロックオーナーが 1〜32 文字で、英数字と . - _ のみからなることを確かめる (FR-048)。
func validateLockOwner(owner string) error {
	if owner == "" {
		return fmt.Errorf("%w: %s is required", ErrInvalidOptions, EnvLockOwner)
	}
	if len(owner) > maxLockOwnerLen {
		return fmt.Errorf("%w: %s must be at most %d characters", ErrInvalidOptions, EnvLockOwner, maxLockOwnerLen)
	}
	for i := 0; i < len(owner); i++ {
		c := owner[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '-', c == '_':
		default:
			return fmt.Errorf("%w: %s may contain only letters, digits, '.', '-' and '_'", ErrInvalidOptions, EnvLockOwner)
		}
	}
	return nil
}
