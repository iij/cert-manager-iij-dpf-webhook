// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"strings"
	"testing"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/testutil"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/tokensource"
)

// FR-008〜010: Secret 不在・key 不在・値が空は恒久的な失敗として再試行せず、到達失敗は
// 一時的な失敗として上限の回数まで再試行する。いずれも分類をログに記録する。
func TestTokenFailureClassification(t *testing.T) {
	const attempts = 3
	cases := []struct {
		name      string
		setup     func(f *fakeSecrets)
		permanent bool
		calls     int
	}{
		{"Secret 不在", func(*fakeSecrets) {}, true, 1},
		{"key 不在", func(f *fakeSecrets) { f.set("team-a", "dpf-token", "other", "v") }, true, 1},
		{"値が空", func(f *fakeSecrets) { f.set("team-a", "dpf-token", "token", "") }, true, 1},
		{"到達失敗", func(f *fakeSecrets) { f.unreachable = true }, false, attempts},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
			f := &fakeSecrets{}
			c.setup(f)
			var logs testutil.LogBuffer
			s := testutil.NewSolverWith(t, srv, tokensource.New(f, tokensource.WithRetry(attempts, 0)), logs.Logger(), nil)
			r := testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge.example.jp.", "KEY",
				testutil.Config(srv, []string{"example.jp"}, nil))
			r.AllowAmbientCredentials = false
			r.ResourceNamespace = "team-a"

			err := s.Present(r)
			if err == nil {
				t.Fatal("Present succeeded")
			}
			if dpf.IsPermanent(err) != c.permanent {
				t.Fatalf("permanent = %v, want %v: %v", dpf.IsPermanent(err), c.permanent, err)
			}
			if got := f.count(); got != c.calls {
				t.Fatalf("Secret was read %d times, want %d", got, c.calls)
			}
			want := "failure=temporary"
			if c.permanent {
				want = "failure=permanent"
			}
			if !strings.Contains(logs.String(), want) {
				t.Fatalf("log does not record %q:\n%s", want, logs.String())
			}
			srv.Lock()
			defer srv.Unlock()
			if len(srv.Tokens) != 0 {
				t.Fatalf("DPF was called %d times without a token", len(srv.Tokens))
			}
		})
	}
}

// 恒久的な失敗ののち Secret を作れば、再起動なしに回復する。
func TestTokenFailureRecoversAfterSecretCreated(t *testing.T) {
	srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
	f := &fakeSecrets{}
	s := testutil.NewSolverWith(t, srv, tokensource.New(f, tokensource.WithRetry(3, 0)),
		(&testutil.LogBuffer{}).Logger(), nil)
	r := testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge.example.jp.", "KEY",
		testutil.Config(srv, []string{"example.jp"}, nil))
	r.AllowAmbientCredentials = false
	r.ResourceNamespace = "team-a"
	if err := s.Present(r); err == nil {
		t.Fatal("Present succeeded without the Secret")
	}
	f.set("team-a", "dpf-token", "token", "tok")
	if err := s.Present(r); err != nil {
		t.Fatal(err)
	}
}
