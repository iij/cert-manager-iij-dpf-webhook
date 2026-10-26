// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"testing"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/testutil"
)

// FR-083〜085 / SC-025: 望む最終状態が現状と一致すれば、排他の取得も反映も行わずに成功する。
func TestNoChangeNoLock(t *testing.T) {
	srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
	s, _ := testutil.NewSolver(t, srv, nil)
	cfg := testutil.Config(srv, []string{"example.jp"}, nil)

	// 回収すべきものが無い CleanUp。
	if err := s.CleanUp(testutil.Request(whapi.ChallengeActionCleanUp, "example.jp.", "_acme-challenge.example.jp.", "KEY", cfg)); err != nil {
		t.Fatal(err)
	}
	srv.Lock()
	puts := srv.ZoneLabelPuts
	srv.Unlock()
	if puts != 0 {
		t.Fatalf("CleanUp with nothing to reclaim acquired the lock")
	}

	present := testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge.example.jp.", "KEY", cfg)
	if err := s.Present(present); err != nil {
		t.Fatal(err)
	}
	srv.Lock()
	puts, atomics := srv.ZoneLabelPuts, srv.Atomics
	srv.Unlock()

	// 閾値内の Present の再試行。
	for i := 0; i < 3; i++ {
		if err := s.Present(present); err != nil {
			t.Fatal(err)
		}
	}
	srv.Lock()
	defer srv.Unlock()
	if srv.ZoneLabelPuts != puts || srv.Atomics != atomics {
		t.Fatalf("retries changed the zone: labelPuts %d->%d atomics %d->%d",
			puts, srv.ZoneLabelPuts, atomics, srv.Atomics)
	}
}
