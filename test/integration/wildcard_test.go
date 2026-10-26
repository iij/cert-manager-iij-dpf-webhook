// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"testing"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/challenge"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/testutil"
)

// US1 受け入れシナリオ 4: ワイルドカード名の証明書では _acme-challenge.example.com に TXT を作る。
func TestWildcard(t *testing.T) {
	srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.com."))
	s, _ := testutil.NewSolver(t, srv, nil)
	r := testutil.Request(whapi.ChallengeActionPresent, "example.com.", "_acme-challenge.example.com.", "KEY",
		testutil.Config(srv, []string{"example.com"}, nil))
	r.DNSName = "*.example.com"
	if err := s.Present(r); err != nil {
		t.Fatal(err)
	}
	srv.Lock()
	defer srv.Unlock()
	rec := srv.Zone("zone0000000001").Find("_acme-challenge.example.com.", "TXT")
	if rec == nil || len(rec.Rdata) != 1 || rec.Rdata[0] != challenge.QuoteTXT("KEY") {
		t.Fatalf("record = %+v", rec)
	}
}
