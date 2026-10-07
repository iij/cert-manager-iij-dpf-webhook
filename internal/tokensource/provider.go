// SPDX-License-Identifier: Apache-2.0

// Package tokensource は DPF アクセストークンを Kubernetes の Secret から取得する。
//
// 取得は github.com/iij/dpf-go/misc/k8s に委ね、自前で Secret を読まない (D-09)。
// namespace は要求ごとに解決した値を必ず明示的に渡し、推測しない。
package tokensource

import (
	"context"
	"errors"
	"time"

	"github.com/iij/dpf-go/misc/k8s"
)

// ErrNamespaceRequired は namespace が空の場合に返される。
var ErrNamespaceRequired = errors.New("tokensource: namespace is required")

// 一時的な失敗に対する再試行の既定 (FR-009)。1 回の読み出しあたりの試行の上限である。
const (
	DefaultMaxAttempts = 3
	DefaultBackoff     = 500 * time.Millisecond
)

// Factory は Secret の出所からトークンを返す関数を作る。
type Factory struct {
	secrets     k8s.SecretsAPI
	maxAttempts int
	backoff     time.Duration
}

// Option は Factory の設定である。
type Option func(*Factory)

// WithRetry は一時的な失敗に対する試行の上限と、試行の間の待ちを指定する。
// maxAttempts が 0 以下なら DefaultMaxAttempts とする。上限の無い再試行はできない (FR-009)。
func WithRetry(maxAttempts int, backoff time.Duration) Option {
	return func(f *Factory) {
		if maxAttempts > 0 {
			f.maxAttempts = maxAttempts
		}
		f.backoff = backoff
	}
}

// New は Factory を返す。
func New(secrets k8s.SecretsAPI, opts ...Option) *Factory {
	f := &Factory{secrets: secrets, maxAttempts: DefaultMaxAttempts, backoff: DefaultBackoff}
	for _, o := range opts {
		o(f)
	}
	return f
}

// IsPermanent は err が恒久的な失敗かを misc/k8s のエラー型で判別する (D-09 / FR-008)。
// Secret・key・値・権限・資格情報の不備は、設定を直さない限り解消しない。
// それ以外 (Kubernetes API への到達失敗など) は一時的な失敗である (FR-009)。
func IsPermanent(err error) bool {
	for _, p := range []error{
		k8s.ErrSecretNotFound, k8s.ErrKeyNotFound, k8s.ErrEmptySecret, k8s.ErrForbidden,
		k8s.ErrNoCredentials, k8s.ErrNamespaceUnknown, k8s.ErrNilClient, ErrNamespaceRequired,
	} {
		if errors.Is(err, p) {
			return true
		}
	}
	return false
}

// Provider は namespace / name / key の Secret から、呼び出しのたびにトークンを読む関数を
// 返す。外部でのローテーションに再起動なしで追随する (FR-011)。
//
// 恒久的な失敗は再試行しない (FR-008)。一時的な失敗は上限の回数まで再試行する (FR-009)。
// dpf-go はトークン取得の失敗を再試行しないため、ここで行う。
func (f *Factory) Provider(namespace, name, key string) (func(context.Context) (string, error), error) {
	if namespace == "" {
		return nil, ErrNamespaceRequired
	}
	read, err := k8s.NewTokenProvider(f.secrets,
		k8s.WithNamespace(namespace), k8s.WithSecretName(name), k8s.WithKey(key))
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (string, error) {
		var err error
		for attempt := 1; ; attempt++ {
			var token string
			token, err = read(ctx)
			if err == nil {
				return token, nil
			}
			if IsPermanent(err) || attempt >= f.maxAttempts {
				return "", err
			}
			if !sleep(ctx, f.backoff) {
				return "", err
			}
		}
	}, nil
}

// sleep は d だけ待つ。context が先に終われば false を返す。
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
