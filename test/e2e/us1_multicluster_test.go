// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package e2e

import (
	"slices"
	"sync"
	"testing"
	"time"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/challenge"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/solver"
)

// T076 (SC-021 / quickstart.md §8): 異なるロックオーナーを持つ 2 つの配備が、同一のチャレンジ
// コードを同時に提示する。チャレンジレコードのラベルは 1 件であり、一方の後片付けで他方の
// RDATA は消えない。
//
// APIService は 1 つの API グループにつき 1 つしか登録できないため、同一クラスタに 2 つの
// 配備を置けない。ここでは 2 つの配備を、ロックオーナーの異なる 2 つの solver で模す。
func TestUS1MultiCluster(t *testing.T) {
	e := loadEnv(t)
	label := "_acme-challenge.e2e-multi"
	name := label + "." + e.zone.String()
	key := "multi-" + time.Now().UTC().Format("150405")

	a := e.newSolver(t, func(o *solver.Options) { o.LockOwner = "e2e-cluster-a" })
	b := e.newSolver(t, func(o *solver.Options) { o.LockOwner = "e2e-cluster-b" })

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, s := range []*solver.Solver{a, b} {
		wg.Add(1)
		go func(i int, s *solver.Solver) {
			defer wg.Done()
			errs[i] = s.Present(e.request(whapi.ChallengeActionPresent, label, key))
		}(i, s)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Present (cluster %d): %v", i, err)
		}
	}

	rec := e.challengeTXT(t, name)
	own := 0
	for k := range rec.Labels {
		if _, ok := challenge.ParseLabelKey(k); ok {
			own++
		}
	}
	if own != 1 {
		t.Errorf("記録のラベルが %d 件 (1 件であるべき): %v", own, rec.Labels)
	}

	// 一方の後片付けで、他方の検証中の RDATA は消えない。
	if err := a.CleanUp(e.request(whapi.ChallengeActionCleanUp, label, key)); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if !slices.Contains(values(e.challengeTXT(t, name)), challenge.QuoteTXT(key)) {
		t.Fatal("一方の後片付けで RDATA が消えた")
	}
}
