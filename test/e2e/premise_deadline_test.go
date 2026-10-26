// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package e2e

import (
	"context"
	"errors"
	"testing"
	"time"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"github.com/iij/dpf-go/utils"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/solver"
)

// T014 (research.md D-15 / SC-004 / SC-013): 操作期限に達した場合、操作期限を越えて書き込まない。
// 解放されなかったロックは TTL の満了で失効し、他者が取得できる。
func TestPremiseDeadline(t *testing.T) {
	e := loadEnv(t)
	ctx := setupContext(t)

	holder := utils.NewMutex(e.api.RecordsAPI, e.api.ZonesAPI, e.zoneID,
		utils.WithOwner("e2e-holder"), utils.WithTTL(40*time.Second))
	if err := holder.LockWait(ctx, lockRetry); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	serial := e.soaSerial(t)

	s := e.newSolver(t, func(o *solver.Options) { o.Deadline = 15 * time.Second; o.LockWait = 2 * time.Second })
	err := s.Present(e.request(whapi.ChallengeActionPresent, "_acme-challenge.e2e-deadline", "deadline"))
	if !errors.Is(err, dpf.ErrDeadline) {
		t.Fatalf("err = %v, want ErrDeadline", err)
	}
	if got := e.soaSerial(t); got != serial {
		t.Errorf("操作期限を越えて書き込んだ: %s -> %s", serial, got)
	}

	// 保持者が解放しないまま TTL が満了すれば、他者が取得できる。
	// solver が操作期限で打ち切った後片付けの完了を待つ余地として、取得の再試行を 30 秒まで許す。
	time.Sleep(45 * time.Second)
	other := utils.NewMutex(e.api.RecordsAPI, e.api.ZonesAPI, e.zoneID, utils.WithOwner("e2e-other"))
	otherCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := other.LockWait(otherCtx, lockRetry); err != nil {
		t.Fatalf("TTL の満了後に取得できない: %v", err)
	}
	_ = other.Unlock(ctx)
}
