// SPDX-License-Identifier: Apache-2.0

package challenge_test

import (
	"testing"
	"time"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/challenge"
)

// apply は計画を現状へ適用した結果を返す (テスト用)。
func apply(cur *challenge.Record, res challenge.Result) *challenge.Record {
	if !res.Changed {
		return cur
	}
	return res.Record
}

// D-15 / SC-005: 呼び出し元がタイムアウトしたのち処理が成功し、再試行が来ても
// RDATA は重複せず、閾値内なら変更しない。期限による回収では「ずれる量」が無い。
func TestRetryAfterTimeoutPresent(t *testing.T) {
	first := mustPlan(t, nil, req(challenge.Present, "A", now))
	state := apply(nil, first)
	retry := mustPlan(t, state, req(challenge.Present, "A", now.Add(2*time.Minute)))
	if retry.Changed {
		t.Fatalf("retry changed the record: %+v", retry.Record)
	}
	if len(state.Values) != 1 || len(state.Labels) != 1 {
		t.Fatalf("state = %+v", state)
	}
}

func TestRetryAfterTimeoutCleanUp(t *testing.T) {
	state := apply(nil, mustPlan(t, nil, req(challenge.Present, "A", now)))
	for i := 0; i < 3; i++ {
		res := mustPlan(t, state, req(challenge.CleanUp, "A", now.Add(time.Duration(i)*time.Minute)))
		if res.Changed {
			t.Fatalf("CleanUp #%d changed the record", i)
		}
	}
	// 期限を過ぎれば回収される。
	res := mustPlan(t, state, req(challenge.CleanUp, "A", now.Add(grace)))
	if !res.Changed || res.Record != nil {
		t.Fatalf("not reclaimed after expiry: %+v", res)
	}
}

// SC-026: 閾値の期間内に同一の提示を 10 回繰り返したとき、変更は 1 回目のみ。
func TestRetryStorm(t *testing.T) {
	var state *challenge.Record
	changes := 0
	for i := 0; i < 10; i++ {
		res := mustPlan(t, state, req(challenge.Present, "A", now.Add(time.Duration(i)*time.Minute)))
		if res.Changed {
			changes++
		}
		state = apply(state, res)
	}
	if changes != 1 {
		t.Fatalf("changes = %d, want 1", changes)
	}
}

// FR-078: RDATA はあるがラベルが無い場合は変更が生じる。
func TestRetryStormMissingLabel(t *testing.T) {
	state := &challenge.Record{Values: []string{q("A")}}
	if res := mustPlan(t, state, req(challenge.Present, "A", now)); !res.Changed {
		t.Fatalf("missing label did not cause a change")
	}
}
