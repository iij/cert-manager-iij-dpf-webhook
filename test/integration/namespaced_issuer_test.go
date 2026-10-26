// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"reflect"
	"testing"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/testutil"
)

// FR-006: namespace 限定の Issuer 由来の要求 (AllowAmbientCredentials が false) では、
// ResourceNamespace の Secret のみを読む。本 Pod の namespace を読まない。
func TestNamespacedIssuerReadsResourceNamespace(t *testing.T) {
	cases := []struct {
		name  string
		extra map[string]any
		want  string
	}{
		{"tokenSecretRef を省略", nil, "team-a/dpf-token/token"},
		{"namespace を省略", map[string]any{"tokenSecretRef": map[string]any{"name": "n", "key": "k"}},
			"team-a/n/k"},
		{"namespace に ResourceNamespace を指定",
			map[string]any{"tokenSecretRef": map[string]any{"namespace": "team-a", "name": "n", "key": "k"}},
			"team-a/n/k"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
			s, tokens := testutil.NewSolver(t, srv, nil)
			r := testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge.example.jp.", "KEY",
				testutil.Config(srv, []string{"example.jp"}, c.extra))
			r.AllowAmbientCredentials = false
			r.ResourceNamespace = "team-a"
			if err := s.Present(r); err != nil {
				t.Fatal(err)
			}
			if got := tokens.Sources(); !reflect.DeepEqual(got, []string{c.want}) {
				t.Fatalf("sources = %v, want [%s]", got, c.want)
			}
		})
	}
}

// 2 つの namespace の Issuer が、それぞれ自分の namespace のトークンで処理される。
func TestNamespacedIssuersUseTheirOwnTokens(t *testing.T) {
	srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
	s, tokens := testutil.NewSolver(t, srv, nil)
	for _, ns := range []string{"team-a", "team-b"} {
		r := testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge."+ns+".example.jp.", "KEY",
			testutil.Config(srv, []string{"example.jp"}, nil))
		r.AllowAmbientCredentials = false
		r.ResourceNamespace = ns
		if err := s.Present(r); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"team-a/dpf-token/token", "team-b/dpf-token/token"}
	if got := tokens.Sources(); !reflect.DeepEqual(got, want) {
		t.Fatalf("sources = %v, want %v", got, want)
	}
}
