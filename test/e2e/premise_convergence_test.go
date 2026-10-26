// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package e2e

import (
	"sync"
	"testing"
	"time"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
)

// T013 (research.md D-15 / SC-027): 呼び出し元がタイムアウトしたのちも処理は完了し、
// 再試行は変更を行わずに成功する。先の処理が走行中に来た再試行は排他を待ち、反映を省く。
func TestPremiseConvergence(t *testing.T) {
	e := loadEnv(t)
	s := e.newSolver(t, nil)
	label := "_acme-challenge.e2e-converge"
	key := "converge-" + time.Now().UTC().Format("20060102150405")

	// 1 回目と、その走行中に来た再試行を並行させる。
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i == 1 {
				time.Sleep(2 * time.Second)
			}
			errs[i] = s.Present(e.request(whapi.ChallengeActionPresent, label, key))
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Present #%d: %v", i, err)
		}
	}

	// 完了後の再試行はゾーンを変更しない (SOA のシリアルが動かない)。
	serial := e.soaSerial(t)
	start := time.Now()
	if err := s.Present(e.request(whapi.ChallengeActionPresent, label, key)); err != nil {
		t.Fatalf("retry: %v", err)
	}
	t.Logf("完了後の再試行: %v", time.Since(start))
	if got := e.soaSerial(t); got != serial {
		t.Errorf("完了後の再試行がゾーンを変更した: %s -> %s", serial, got)
	}
}
