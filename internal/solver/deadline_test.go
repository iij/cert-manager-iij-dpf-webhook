// SPDX-License-Identifier: Apache-2.0

package solver

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
)

// FR-057: 操作期限の既定は 60 秒より長い (5 分)。
func TestDeadlineDefaultLongerThanRequestTimeout(t *testing.T) {
	if DefaultOptions().Deadline != 5*time.Minute || DefaultOptions().Deadline <= 60*time.Second {
		t.Fatalf("Deadline = %v", DefaultOptions().Deadline)
	}
}

// FR-057a: 操作期限の context は要求の context から派生しない。
func TestOperationContextIsDetached(t *testing.T) {
	o := DefaultOptions()
	ctx, cancel := operationContext(o)
	defer cancel()
	dl, ok := ctx.Deadline()
	if !ok || time.Until(dl) < o.Deadline-time.Second {
		t.Fatalf("deadline = %v, %v", dl, ok)
	}
	if ctx.Err() != nil {
		t.Fatalf("context already done")
	}
}

// FR-058 / FR-059: 操作期限は DPF への呼び出しまで伝播し、操作期限を越えて書き込まない。
// 他者がロックを保持し続けるゾーンでは、待機が操作期限で打ち切られ、反映は行われない。
func TestDeadlineStopsWrites(t *testing.T) {
	z := dpftest.StandardZone("zone0000000001", "example.jp.")
	// 他者が 1 時間保持するロック。
	z.Labels = map[string]string{"lock.dpf-go": "other-" + strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)}
	srv := dpftest.New(t, z)

	o := testOptions()
	o.Deadline = 300 * time.Millisecond
	o.LockWait = 50 * time.Millisecond
	s := newTestSolver(t, srv, o)

	start := time.Now()
	err := s.Present(challengeRequest("example.jp.", "_acme-challenge.example.jp.", "KEY", `{"domainFilters":["example.jp"]}`))
	if !errors.Is(err, dpf.ErrDeadline) {
		t.Fatalf("err = %v, want ErrDeadline", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("did not stop at the deadline: %v", elapsed)
	}
	srv.Lock()
	defer srv.Unlock()
	if srv.Atomics != 0 {
		t.Fatalf("wrote after the deadline")
	}
	// 他者のロックを奪っていない (FR-051)。
	if v := srv.Zone("zone0000000001").Labels["lock.dpf-go"]; v[:6] != "other-" {
		t.Errorf("lock was taken: %q", v)
	}
}
