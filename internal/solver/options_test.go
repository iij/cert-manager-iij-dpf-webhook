// SPDX-License-Identifier: Apache-2.0

package solver

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestOptionsDefaults(t *testing.T) {
	o, err := OptionsFromEnv(env(map[string]string{EnvLockOwner: "cluster-a", EnvPodNamespace: "cert-manager"}))
	if err != nil {
		t.Fatal(err)
	}
	if o.Grace != time.Hour || o.Threshold != 30*time.Minute || o.Deadline != 5*time.Minute ||
		o.LockTTL != 2*time.Minute || o.LockWait != 5*time.Second {
		t.Fatalf("defaults = %+v", o)
	}
}

func TestOptionsOverrides(t *testing.T) {
	o, err := OptionsFromEnv(env(map[string]string{
		EnvLockOwner: "c", EnvPodNamespace: "ns", EnvExpireGrace: "2h", EnvOperationDeadline: "3m",
		EnvLockTTL: "90s", EnvLockWait: "1s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	// 閾値を指定しなければ猶予の 1/2 とする。
	if o.Grace != 2*time.Hour || o.Threshold != time.Hour || o.Deadline != 3*time.Minute ||
		o.LockTTL != 90*time.Second || o.LockWait != time.Second {
		t.Fatalf("options = %+v", o)
	}
}

// FR-047〜050 / FR-080: 起動を中止すべき設定。
func TestOptionsReject(t *testing.T) {
	base := map[string]string{EnvLockOwner: "cluster-a", EnvPodNamespace: "ns"}
	with := func(k, v string) map[string]string {
		m := map[string]string{}
		for a, b := range base {
			m[a] = b
		}
		m[k] = v
		return m
	}
	cases := map[string]map[string]string{
		"ロックオーナー未設定":          with(EnvLockOwner, ""),
		"ロックオーナー 33 文字":       with(EnvLockOwner, strings.Repeat("a", 33)),
		"許されない文字":             with(EnvLockOwner, "cluster/a"),
		"空白":                  with(EnvLockOwner, "cluster a"),
		"猶予が非正":               with(EnvExpireGrace, "0s"),
		"操作期限が非正":             with(EnvOperationDeadline, "-1s"),
		"ロック TTL が非正":         with(EnvLockTTL, "0"),
		"閾値が猶予以上":             with(EnvExpireThreshold, "1h"),
		"期間を解釈できない":           with(EnvLockTTL, "two minutes"),
		"Pod の namespace 未設定": with(EnvPodNamespace, ""),
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := OptionsFromEnv(env(m)); !errors.Is(err, ErrInvalidOptions) {
				t.Fatalf("err = %v, want ErrInvalidOptions", err)
			}
		})
	}
}

// 32 文字ちょうどは受け付ける。
func TestOptionsOwner32(t *testing.T) {
	if _, err := OptionsFromEnv(env(map[string]string{
		EnvLockOwner: strings.Repeat("a", 32), EnvPodNamespace: "ns",
	})); err != nil {
		t.Fatal(err)
	}
}
