// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"strconv"
	"testing"
	"time"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/challenge"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/testutil"
)

const fqdn = "_acme-challenge.example.jp."

// FR-082: CleanUp は当該チャレンジの RDATA と記録を削除しない。
func TestCleanUpDoesNotDelete(t *testing.T) {
	srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
	s, _ := testutil.NewSolver(t, srv, nil)
	cfg := testutil.Config(srv, []string{"example.jp"}, nil)
	if err := s.Present(testutil.Request(whapi.ChallengeActionPresent, "example.jp.", fqdn, "KEY", cfg)); err != nil {
		t.Fatal(err)
	}
	if err := s.CleanUp(testutil.Request(whapi.ChallengeActionCleanUp, "example.jp.", fqdn, "KEY", cfg)); err != nil {
		t.Fatal(err)
	}
	srv.Lock()
	defer srv.Unlock()
	rec := srv.Zone("zone0000000001").Find(fqdn, "TXT")
	if rec == nil || rec.Rdata[0] != challenge.QuoteTXT("KEY") {
		t.Fatalf("CleanUp removed the record: %+v", rec)
	}
}

// FR-072〜075 / FR-035: 期限切れは回収され、RDATA が空になればレコードごと削除される。
// 他者の RDATA は維持される。
func TestCleanUpReclaimsExpired(t *testing.T) {
	z := dpftest.StandardZone("zone0000000001", "example.jp.")
	expired := strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10)
	z.Records = append(z.Records,
		&dpftest.Record{Name: fqdn, Rrtype: "TXT", Rdata: []string{challenge.QuoteTXT("OLD")},
			Labels: map[string]string{challenge.LabelKey(challenge.TargetID("OLD")): expired}},
		&dpftest.Record{Name: "_acme-challenge.www.example.jp.", Rrtype: "TXT",
			Rdata:  []string{`"foreign"`, challenge.QuoteTXT("OLD2")},
			Labels: map[string]string{challenge.LabelKey(challenge.TargetID("OLD2")): expired}},
	)
	srv := dpftest.NewTLS(t, z)
	s, _ := testutil.NewSolver(t, srv, nil)
	cfg := testutil.Config(srv, []string{"example.jp"}, nil)

	if err := s.CleanUp(testutil.Request(whapi.ChallengeActionCleanUp, "example.jp.", fqdn, "KEY", cfg)); err != nil {
		t.Fatal(err)
	}
	if err := s.CleanUp(testutil.Request(whapi.ChallengeActionCleanUp, "example.jp.", "_acme-challenge.www.example.jp.", "KEY", cfg)); err != nil {
		t.Fatal(err)
	}
	srv.Lock()
	defer srv.Unlock()
	if srv.Zone("zone0000000001").Find(fqdn, "TXT") != nil {
		t.Fatal("record with only expired values remains")
	}
	www := srv.Zone("zone0000000001").Find("_acme-challenge.www.example.jp.", "TXT")
	if www == nil || len(www.Rdata) != 1 || www.Rdata[0] != `"foreign"` || len(www.Labels) != 0 {
		t.Fatalf("www record = %+v", www)
	}
}

// FR-038: Present を伴わない CleanUp は成功する。
func TestCleanUpWithoutPresent(t *testing.T) {
	srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
	s, _ := testutil.NewSolver(t, srv, nil)
	if err := s.CleanUp(testutil.Request(whapi.ChallengeActionCleanUp, "example.jp.", fqdn, "KEY",
		testutil.Config(srv, []string{"example.jp"}, nil))); err != nil {
		t.Fatal(err)
	}
}
