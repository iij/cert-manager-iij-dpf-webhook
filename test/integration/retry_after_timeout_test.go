// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"sync"
	"testing"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/testutil"
)

// SC-005 / SC-027: 先の処理が走行中に再試行が来ても、記録と RDATA の数はずれず、
// 再試行は排他を待ってから反映を省く。
func TestRetryWhileRunning(t *testing.T) {
	srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
	s, _ := testutil.NewSolver(t, srv, nil)
	cfg := testutil.Config(srv, []string{"example.jp"}, nil)
	present := testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge.example.jp.", "KEY", cfg)

	// 1 回目の反映の直前に、再試行を走らせる。再試行は排他を待つ。
	var wg sync.WaitGroup
	retryErr := make(chan error, 1)
	var once sync.Once
	srv.BeforeAtomic = func(*dpftest.Server) {
		once.Do(func() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				retryErr <- s.Present(present)
			}()
		})
	}
	if err := s.Present(present); err != nil {
		t.Fatalf("first Present: %v", err)
	}
	wg.Wait()
	if err := <-retryErr; err != nil {
		t.Fatalf("retry: %v", err)
	}

	cleanup := testutil.Request(whapi.ChallengeActionCleanUp, "example.jp.", "_acme-challenge.example.jp.", "KEY", cfg)
	for i := 0; i < 2; i++ {
		if err := s.CleanUp(cleanup); err != nil {
			t.Fatalf("CleanUp: %v", err)
		}
	}

	srv.Lock()
	defer srv.Unlock()
	if srv.Atomics != 1 {
		t.Fatalf("Atomics = %d, want 1", srv.Atomics)
	}
	rec := srv.Zone("zone0000000001").Find("_acme-challenge.example.jp.", "TXT")
	if rec == nil || len(rec.Rdata) != 1 || len(rec.Labels) != 1 {
		t.Fatalf("record = %+v", rec)
	}
}
