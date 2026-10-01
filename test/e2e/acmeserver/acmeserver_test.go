// SPDX-License-Identifier: Apache-2.0

package acmeserver

import (
	"errors"
	"testing"
)

func env(v string) func(string) string {
	return func(k string) string {
		if k == EnvACMEServer {
			return v
		}
		return ""
	}
}

// 憲章「ACME サーバの利用」/ research.md D-17: 未設定なら Let's Encrypt のステージング環境。
func TestDefaultIsStaging(t *testing.T) {
	got, err := Resolve(env(""))
	if err != nil || got != "https://acme-staging-v02.api.letsencrypt.org/directory" {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
}

func TestAllowed(t *testing.T) {
	got, err := Resolve(env("https://acme-staging-v02.api.letsencrypt.org/directory"))
	if err != nil || got != "https://acme-staging-v02.api.letsencrypt.org/directory" {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
}

// 本番環境の一覧の値や未知の値では e2e を開始しない。
func TestRejected(t *testing.T) {
	for _, v := range []string{
		"https://acme-v02.api.letsencrypt.org/directory",
		"https://dv.acme-v02.api.pki.goog/directory",
		"https://acme.zerossl.com/v2/DV90",
		"https://api.buypass.com/acme/directory",
		"https://acme.example-ca.test/directory",
	} {
		if _, err := Resolve(env(v)); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("Resolve(%q) = %v, want ErrNotAllowed", v, err)
		}
	}
}
