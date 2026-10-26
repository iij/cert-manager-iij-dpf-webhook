// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"strings"
	"testing"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/testutil"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/tokensource"
)

// FR-011: 外部でトークンがローテーションされたら、再起動なしに新しい値を使う。
func TestTokenRotation(t *testing.T) {
	srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
	f := &fakeSecrets{}
	f.set("team-a", "dpf-token", "token", "token-before")
	s := testutil.NewSolverWith(t, srv, tokensource.New(f), (&testutil.LogBuffer{}).Logger(), nil)
	cfg := testutil.Config(srv, []string{"example.jp"}, nil)

	present := func(fqdn string) []string {
		t.Helper()
		srv.Lock()
		from := len(srv.Tokens)
		srv.Unlock()
		r := testutil.Request(whapi.ChallengeActionPresent, "example.jp.", fqdn, "KEY", cfg)
		r.AllowAmbientCredentials = false
		r.ResourceNamespace = "team-a"
		if err := s.Present(r); err != nil {
			t.Fatal(err)
		}
		srv.Lock()
		defer srv.Unlock()
		return append([]string(nil), srv.Tokens[from:]...)
	}

	for _, h := range present("_acme-challenge.a.example.jp.") {
		if !strings.Contains(h, "token-before") {
			t.Fatalf("Authorization = %q before rotation", h)
		}
	}
	f.set("team-a", "dpf-token", "token", "token-after")
	got := present("_acme-challenge.b.example.jp.")
	if len(got) == 0 {
		t.Fatal("no DPF request after rotation")
	}
	for _, h := range got {
		if !strings.Contains(h, "token-after") {
			t.Fatalf("Authorization = %q after rotation", h)
		}
	}
}
