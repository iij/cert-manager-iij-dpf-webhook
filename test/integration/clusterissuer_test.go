// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"reflect"
	"testing"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/testutil"
)

// FR-006: ClusterIssuer 由来の要求では、tokenSecretRef.namespace、省略時は本 Pod の namespace の
// Secret を読む。
func TestClusterIssuerTokenNamespace(t *testing.T) {
	cases := []struct {
		name  string
		extra map[string]any
		want  string
	}{
		{"指定あり", map[string]any{"tokenSecretRef": map[string]any{"namespace": "secrets", "name": "n", "key": "k"}},
			"secrets/n/k"},
		{"省略時は本 Pod の namespace", nil, testutil.PodNamespace + "/dpf-token/token"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
			s, tokens := testutil.NewSolver(t, srv, nil)
			r := testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge.example.jp.", "KEY",
				testutil.Config(srv, []string{"example.jp"}, c.extra))
			r.ResourceNamespace = "cert-manager-elsewhere"
			if err := s.Present(r); err != nil {
				t.Fatal(err)
			}
			if got := tokens.Sources(); !reflect.DeepEqual(got, []string{c.want}) {
				t.Fatalf("sources = %v, want [%s]", got, c.want)
			}
		})
	}
}
