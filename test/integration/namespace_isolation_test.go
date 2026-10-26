// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"errors"
	"testing"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/solver"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/testutil"
)

// FR-007 / SC-008: namespace 限定の Issuer が他の namespace の Secret を指していれば要求を
// 失敗させる。ResourceNamespace へ読み替えず、どの Secret も読まず、DPF にも触れない。
func TestNamespacedIssuerCannotReachOtherNamespace(t *testing.T) {
	for _, other := range []string{"team-b", testutil.PodNamespace} {
		t.Run(other, func(t *testing.T) {
			srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
			s, tokens := testutil.NewSolver(t, srv, nil)
			for _, action := range []whapi.ChallengeAction{whapi.ChallengeActionPresent, whapi.ChallengeActionCleanUp} {
				r := testutil.Request(action, "example.jp.", "_acme-challenge.example.jp.", "KEY",
					testutil.Config(srv, []string{"example.jp"},
						map[string]any{"tokenSecretRef": map[string]any{"namespace": other}}))
				r.AllowAmbientCredentials = false
				r.ResourceNamespace = "team-a"
				var err error
				if action == whapi.ChallengeActionPresent {
					err = s.Present(r)
				} else {
					err = s.CleanUp(r)
				}
				if !errors.Is(err, solver.ErrNamespaceNotAllowed) {
					t.Fatalf("%s: err = %v, want ErrNamespaceNotAllowed", action, err)
				}
			}
			if got := tokens.Sources(); len(got) != 0 {
				t.Fatalf("secrets were read: %v", got)
			}
			srv.Lock()
			defer srv.Unlock()
			if len(srv.Tokens) != 0 {
				t.Fatalf("DPF was called %d times", len(srv.Tokens))
			}
		})
	}
}
