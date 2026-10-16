// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package e2e

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	sdk "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/utils"
)

// T012 (research.md §4 1 / 1b / 5a): 排他の取得・反映・解放の所要時間を記録する。
// 60 秒に収まることは求めない。記録は操作期限の既定 (5 分) の妥当性の根拠とする。
func TestPremiseTiming(t *testing.T) {
	e := loadEnv(t)
	ctx := setupContext(t)

	// 1b: アクセストークンでゾーンのラベルを読み書きできる。
	before, _, err := e.api.ZonesAPI.GetZoneLabels(ctx, e.zoneID).Execute()
	if err != nil {
		t.Fatalf("GetZoneLabels: %v", err)
	}
	t.Logf("zone labels: %d", len(before.Result.Labels))

	mu := utils.NewMutex(e.api.RecordsAPI, e.api.ZonesAPI, e.zoneID,
		utils.WithOwner("e2e-timing"), utils.WithTTL(2*time.Minute))
	// 前の検証の解放の後片付けを待つことがあるため、取得の時間は待ちを含む上限の目安である。
	start := time.Now()
	if err := mu.LockWait(ctx, lockRetry); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	lock := time.Since(start)

	start = time.Now()
	if err := mu.Unlock(ctx); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	unlock := time.Since(start)

	ap := e.applier(utils.WithOwner("e2e-timing"), utils.WithTTL(2*time.Minute))
	start = time.Now()
	err = ap.Apply(ctx, func(_ context.Context, rs []sdk.OverwriteRecordsInner) ([]sdk.OverwriteRecordsInner, error) {
		return rs, nil
	}, utils.WithApplyDescription("e2e timing"))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	apply := time.Since(start)

	after, _, err := e.api.ZonesAPI.GetZoneLabels(ctx, e.zoneID).Execute()
	if err != nil {
		t.Fatalf("GetZoneLabels: %v", err)
	}
	for k, v := range before.Result.Labels {
		if k != utils.ZoneLockLabelKey && after.Result.Labels[k] != v {
			t.Errorf("利用者のゾーンのラベル %q が変わった", k)
		}
	}

	t.Logf("排他の取得: %v / 解放 (確定を待たない): %v / 取得・反映・解放: %v (反映のみの推定 %v)",
		lock, unlock, apply, apply-lock-unlock)
}

// research.md §4 5a: 別のアクセストークンを用いる 2 つのプロセスの間で同時取得が起こらない。
func TestPremiseCrossTokenExclusion(t *testing.T) {
	e := loadEnv(t)
	if e.token2 == "" {
		t.Skip("DPF_API_TOKEN_2 が必要")
	}
	ctx := context.Background()
	a := utils.NewMutex(e.api.RecordsAPI, e.api.ZonesAPI, e.zoneID, utils.WithOwner("e2e-a"))
	other := e.client(t, e.token2)
	b := utils.NewMutex(other.RecordsAPI, other.ZonesAPI, e.zoneID, utils.WithOwner("e2e-b"))

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, m := range []*utils.Mutex{a, b} {
		wg.Add(1)
		go func(i int, m *utils.Mutex) {
			defer wg.Done()
			errs[i] = m.Lock(ctx)
		}(i, m)
	}
	wg.Wait()
	acquired := 0
	for i, err := range errs {
		switch {
		case err == nil:
			acquired++
			defer func(m *utils.Mutex) { _ = m.Unlock(ctx) }([]*utils.Mutex{a, b}[i])
		case !errors.Is(err, utils.ErrStillLock):
			t.Errorf("Lock #%d: %v", i, err)
		}
	}
	if acquired != 1 {
		t.Fatalf("取得できたのが %d 者 (1 者であるべき)", acquired)
	}
}
