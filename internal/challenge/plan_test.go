// SPDX-License-Identifier: Apache-2.0

package challenge_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/challenge"
)

var (
	now       = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	grace     = time.Hour
	threshold = 30 * time.Minute
)

func req(action challenge.Action, key string, at time.Time) challenge.Request {
	return challenge.Request{Action: action, Key: key, Now: at, Grace: grace, Threshold: threshold}
}

func lk(key string) string   { return challenge.LabelKey(challenge.TargetID(key)) }
func q(key string) string    { return challenge.QuoteTXT(key) }
func exp(t time.Time) string { return challenge.EncodeExpiry(t) }

func mustPlan(t *testing.T, cur *challenge.Record, r challenge.Request) challenge.Result {
	t.Helper()
	res, err := challenge.Plan(cur, r)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	return res
}

// 不在 → 存在: 最初の Present で RDATA とラベルを 1 回の操作で追加する。
func TestPresentCreates(t *testing.T) {
	res := mustPlan(t, nil, req(challenge.Present, "A", now))
	if !res.Changed || res.Record == nil {
		t.Fatalf("Changed=%v Record=%v", res.Changed, res.Record)
	}
	if !reflect.DeepEqual(res.Record.Values, []string{q("A")}) {
		t.Errorf("Values = %v", res.Record.Values)
	}
	if got := res.Record.Labels[lk("A")]; got != exp(now.Add(grace)) {
		t.Errorf("label = %q", got)
	}
	if !res.Created {
		t.Errorf("Created = false")
	}
}

// FR-029 / FR-069: 既存の RDATA と他者のラベルを維持して追記する。
func TestPresentAppendsKeepingOthers(t *testing.T) {
	cur := &challenge.Record{
		Values: []string{`"v=spf1 -all"`, q("B")},
		Labels: map[string]string{"spf-managed-by": "someone", lk("B"): exp(now.Add(50 * time.Minute))},
	}
	res := mustPlan(t, cur, req(challenge.Present, "A", now))
	if !reflect.DeepEqual(res.Record.Values, []string{`"v=spf1 -all"`, q("B"), q("A")}) {
		t.Errorf("Values = %v", res.Record.Values)
	}
	if res.Record.Labels["spf-managed-by"] != "someone" || res.Record.Labels[lk("B")] != exp(now.Add(50*time.Minute)) {
		t.Errorf("others' labels changed: %v", res.Record.Labels)
	}
	if res.Created {
		t.Errorf("Created = true for an existing record")
	}
	// 入力を書き換えない。
	if len(cur.Values) != 2 || len(cur.Labels) != 2 {
		t.Errorf("input was mutated: %+v", cur)
	}
}

// FR-030 / FR-079: 既に揃い、残りが閾値以上なら変更しない。
func TestPresentNoChangeWithinThreshold(t *testing.T) {
	cur := &challenge.Record{
		Values: []string{q("A")},
		Labels: map[string]string{lk("A"): exp(now.Add(31 * time.Minute))},
	}
	if res := mustPlan(t, cur, req(challenge.Present, "A", now)); res.Changed {
		t.Fatalf("Changed = true within threshold: %+v", res.Record)
	}
}

// FR-079: 残りが閾値を下回れば期限のみを更新する。RDATA は重複させない。
func TestPresentRenewsBelowThreshold(t *testing.T) {
	cur := &challenge.Record{
		Values: []string{q("A")},
		Labels: map[string]string{lk("A"): exp(now.Add(29 * time.Minute))},
	}
	res := mustPlan(t, cur, req(challenge.Present, "A", now))
	if !res.Changed {
		t.Fatalf("Changed = false below threshold")
	}
	if !reflect.DeepEqual(res.Record.Values, []string{q("A")}) {
		t.Errorf("Values = %v", res.Record.Values)
	}
	if res.Record.Labels[lk("A")] != exp(now.Add(grace)) {
		t.Errorf("label not renewed: %v", res.Record.Labels)
	}
}

// FR-078: RDATA はあるがラベルが無い場合はラベルを追加する。
func TestPresentAddsMissingLabel(t *testing.T) {
	cur := &challenge.Record{Values: []string{q("A")}, Labels: map[string]string{}}
	res := mustPlan(t, cur, req(challenge.Present, "A", now))
	if !res.Changed || res.Record.Labels[lk("A")] == "" {
		t.Fatalf("label not added: %+v", res)
	}
	if !reflect.DeepEqual(res.Record.Values, []string{q("A")}) {
		t.Errorf("Values = %v", res.Record.Values)
	}
}

// FR-082 / FR-083: 回収すべきものが無い CleanUp は変更しない。当該コードも削除しない。
func TestCleanUpKeepsUnexpired(t *testing.T) {
	cur := &challenge.Record{
		Values: []string{q("A")},
		Labels: map[string]string{lk("A"): exp(now.Add(10 * time.Minute))},
	}
	if res := mustPlan(t, cur, req(challenge.CleanUp, "A", now)); res.Changed {
		t.Fatalf("CleanUp changed an unexpired record: %+v", res.Record)
	}
}

// FR-038: レコードが無い CleanUp は成功し、変更しない。
func TestCleanUpAbsent(t *testing.T) {
	if res := mustPlan(t, nil, req(challenge.CleanUp, "A", now)); res.Changed {
		t.Fatalf("Changed = true for an absent record")
	}
}

// FR-072 / FR-073: 期限切れのラベルと、それに対応する RDATA を取り除く。
func TestReclaimExpired(t *testing.T) {
	cur := &challenge.Record{
		Values: []string{`"other"`, q("OLD"), q("A")},
		Labels: map[string]string{
			lk("OLD"): exp(now.Add(-time.Second)),
			lk("A"):   exp(now.Add(50 * time.Minute)),
			"foreign": "x",
		},
	}
	res := mustPlan(t, cur, req(challenge.CleanUp, "A", now))
	if !res.Changed {
		t.Fatalf("Changed = false")
	}
	if !reflect.DeepEqual(res.Record.Values, []string{`"other"`, q("A")}) {
		t.Errorf("Values = %v", res.Record.Values)
	}
	if _, ok := res.Record.Labels[lk("OLD")]; ok {
		t.Errorf("expired label remains")
	}
	if res.Record.Labels["foreign"] != "x" || res.Record.Labels[lk("A")] == "" {
		t.Errorf("labels = %v", res.Record.Labels)
	}
	if res.Reclaimed != 1 {
		t.Errorf("Reclaimed = %d", res.Reclaimed)
	}
}

// FR-074: 対応するラベルを持たない RDATA は回収しない。
func TestNoReclaimWithoutLabel(t *testing.T) {
	cur := &challenge.Record{Values: []string{q("X")}, Labels: map[string]string{}}
	if res := mustPlan(t, cur, req(challenge.CleanUp, "A", now)); res.Changed {
		t.Fatalf("reclaimed a value without a label: %+v", res.Record)
	}
}

// FR-075: 期限切れのラベルは対応する RDATA の有無によらず取り除く。
func TestReclaimOrphanLabel(t *testing.T) {
	cur := &challenge.Record{
		Values: []string{`"other"`},
		Labels: map[string]string{lk("GONE"): exp(now.Add(-time.Minute))},
	}
	res := mustPlan(t, cur, req(challenge.CleanUp, "A", now))
	if !res.Changed || len(res.Record.Labels) != 0 {
		t.Fatalf("orphan label not removed: %+v", res)
	}
	if !reflect.DeepEqual(res.Record.Values, []string{`"other"`}) {
		t.Errorf("Values = %v", res.Record.Values)
	}
}

// FR-035: RDATA が 1 つも残らない場合はレコードごと削除する計画を返す。
func TestReclaimDeletesEmptyRecord(t *testing.T) {
	cur := &challenge.Record{
		Values: []string{q("OLD")},
		Labels: map[string]string{lk("OLD"): exp(now.Add(-time.Second))},
	}
	res := mustPlan(t, cur, req(challenge.CleanUp, "A", now))
	if !res.Changed || res.Record != nil {
		t.Fatalf("record not deleted: %+v", res)
	}
}

// Present でも最初に回収する (FR-072)。
func TestPresentReclaimsFirst(t *testing.T) {
	cur := &challenge.Record{
		Values: []string{q("OLD")},
		Labels: map[string]string{lk("OLD"): exp(now.Add(-time.Second))},
	}
	res := mustPlan(t, cur, req(challenge.Present, "A", now))
	if !reflect.DeepEqual(res.Record.Values, []string{q("A")}) {
		t.Errorf("Values = %v", res.Record.Values)
	}
	if _, ok := res.Record.Labels[lk("OLD")]; ok {
		t.Errorf("expired label remains")
	}
}

// FR-086: 総数が 10 を超える場合は失敗させ、追い出さない。
func TestLabelLimit(t *testing.T) {
	labels := map[string]string{}
	for i := 0; i < 10; i++ {
		labels[fmt.Sprintf("foreign-%d", i)] = "x"
	}
	cur := &challenge.Record{Values: []string{`"other"`}, Labels: labels}
	_, err := challenge.Plan(cur, req(challenge.Present, "A", now))
	if !errors.Is(err, challenge.ErrLabelLimit) {
		t.Fatalf("err = %v, want ErrLabelLimit", err)
	}
	if len(cur.Labels) != 10 {
		t.Errorf("input labels mutated")
	}
}

// 回収によって枠が空けば上限に達しない。
func TestLabelLimitAfterReclaim(t *testing.T) {
	labels := map[string]string{lk("OLD"): exp(now.Add(-time.Second))}
	for i := 0; i < 9; i++ {
		labels[fmt.Sprintf("foreign-%d", i)] = "x"
	}
	cur := &challenge.Record{Values: []string{q("OLD")}, Labels: labels}
	if _, err := challenge.Plan(cur, req(challenge.Present, "A", now)); err != nil {
		t.Fatalf("Plan: %v", err)
	}
}

// 不正な要求は失敗させる。
func TestInvalidRequest(t *testing.T) {
	bad := []challenge.Request{
		{Action: challenge.Present, Key: "", Now: now, Grace: grace, Threshold: threshold},
		{Action: 0, Key: "A", Now: now, Grace: grace, Threshold: threshold},
		{Action: challenge.Present, Key: "A", Now: now, Grace: grace, Threshold: grace},
	}
	for _, r := range bad {
		if _, err := challenge.Plan(nil, r); err == nil {
			t.Errorf("Plan(%+v) succeeded", r)
		}
	}
}
