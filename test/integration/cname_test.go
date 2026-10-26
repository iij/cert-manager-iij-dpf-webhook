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

func delegatedServer(t *testing.T) *dpftest.Server {
	return dpftest.NewTLS(t,
		dpftest.StandardZone("zone00000000jp", "example.jp."),
		dpftest.StandardZone("zone0delegated", "delegated.example."),
	)
}

// FR-013 / FR-017 / FR-040 / FR-041: CNAME で委譲された要求では、委譲先の ResolvedZone と一致する
// ゾーンに対して domainFilters を判定し、渡された名前 (_acme-challenge で始まらない) に書く。
func TestCNAMEDelegationAllowed(t *testing.T) {
	srv := delegatedServer(t)
	s, _ := testutil.NewSolver(t, srv, nil)
	r := testutil.Request(whapi.ChallengeActionPresent, "delegated.example.", "hoge.delegated.example.", "KEY",
		testutil.Config(srv, []string{"delegated.example"}, nil))
	r.DNSName = "example.jp"
	if err := s.Present(r); err != nil {
		t.Fatal(err)
	}
	srv.Lock()
	defer srv.Unlock()
	if srv.Zone("zone0delegated").Find("hoge.delegated.example.", "TXT") == nil {
		t.Fatal("record was not written to the delegated zone")
	}
}

// FR-017: 委譲元が許可されていても、委譲先が不許可なら失敗し、何も変更しない。
func TestCNAMEDelegationDenied(t *testing.T) {
	srv := delegatedServer(t)
	s, _ := testutil.NewSolver(t, srv, nil)
	r := testutil.Request(whapi.ChallengeActionPresent, "delegated.example.", "hoge.delegated.example.", "KEY",
		testutil.Config(srv, []string{"example.jp"}, nil))
	if err := s.Present(r); !errors.Is(err, solver.ErrZoneNotAllowed) {
		t.Fatalf("err = %v, want ErrZoneNotAllowed", err)
	}
	srv.Lock()
	defer srv.Unlock()
	if srv.Atomics != 0 || srv.ZoneLabelPuts != 0 {
		t.Fatalf("zone was touched: atomics=%d labelPuts=%d", srv.Atomics, srv.ZoneLabelPuts)
	}
}

// FR-019: domainFilters が空なら、いかなるゾーンも変更しない。
func TestEmptyDomainFilters(t *testing.T) {
	srv := delegatedServer(t)
	s, _ := testutil.NewSolver(t, srv, nil)
	r := testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge.example.jp.", "KEY",
		testutil.Config(srv, nil, nil))
	if err := s.Present(r); !errors.Is(err, solver.ErrZoneNotAllowed) {
		t.Fatalf("err = %v, want ErrZoneNotAllowed", err)
	}
	srv.Lock()
	defer srv.Unlock()
	if srv.Atomics != 0 || srv.ZoneLabelPuts != 0 {
		t.Fatalf("zone was touched")
	}
}
