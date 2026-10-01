// SPDX-License-Identifier: Apache-2.0

package dpf_test

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/challenge"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
)

const (
	testZone  = "example.jp."
	testFQDN  = "_acme-challenge.example.jp."
	testOwner = "cluster-a"
)

func i32(v int32) *int32 { return &v }

// applyZone は本サービスと無関係なレコードを含むゾーンを返す。
func applyZone() *dpftest.Zone {
	z := dpftest.StandardZone("z1", testZone)
	z.Records = append(z.Records,
		// TTL が null、ラベル付き、コメント付きの無関係なレコード。
		&dpftest.Record{Name: "www.example.jp.", Rrtype: "A", Rdata: []string{"192.0.2.1"},
			Labels: map[string]string{"team": "web"}, Description: "web server"},
		&dpftest.Record{Name: "mail.example.jp.", TTL: i32(600), Rrtype: "MX", Rdata: []string{"10 mx.example.jp."}},
		// 対象名の TXT 以外の種別。
		&dpftest.Record{Name: testFQDN, TTL: i32(300), Rrtype: "CAA", Rdata: []string{`0 issue "letsencrypt.org"`}},
	)
	return z
}

func target() (dpf.Zone, dnsname.Name) {
	return dpf.Zone{ID: "z1", Name: dnsname.MustParse(testZone)}, dnsname.MustParse(testFQDN)
}

func opts() dpf.ApplyOptions {
	return dpf.ApplyOptions{Owner: testOwner, LockTTL: dpf.DefaultLockTTL, LockWait: dpf.DefaultLockWait}
}

func noRecheck() error { return nil }

func presentPlan(key string) dpf.Planner {
	return func(cur *challenge.Record) (challenge.Result, error) {
		return challenge.Plan(cur, challenge.Request{Action: challenge.Present, Key: key,
			Now: time.Now(), Grace: time.Hour, Threshold: 30 * time.Minute})
	}
}

// snapshot は対象名の TXT 以外のレコードを比較できる形にする。
func snapshot(z *dpftest.Zone) map[string]dpftest.Record {
	out := map[string]dpftest.Record{}
	for _, r := range z.Records {
		if r.Name == testFQDN && r.Rrtype == "TXT" {
			continue
		}
		c := *r
		c.ID = ""
		out[r.Name+"/"+r.Rrtype] = c
	}
	return out
}

// FR-055 / FR-056: 対象以外のレコードを変えず、SOA と Zone Apex の NS を取り込まない。
func TestApplyKeepsOtherRecords(t *testing.T) {
	srv := dpftest.New(t, applyZone())
	c := newClient(t, srv)
	srv.Lock()
	before := snapshot(srv.Zone("z1"))
	srv.Unlock()

	zone, fqdn := target()
	applied, err := c.Apply(context.Background(), zone, fqdn, opts(), noRecheck, presentPlan("KEY"))
	if err != nil || !applied {
		t.Fatalf("Apply = %v, %v", applied, err)
	}

	srv.Lock()
	defer srv.Unlock()
	if srv.Atomics != 1 {
		t.Fatalf("Atomics = %d", srv.Atomics)
	}
	if got := snapshot(srv.Zone("z1")); !reflect.DeepEqual(got, before) {
		t.Errorf("other records changed:\n got  %+v\n want %+v", got, before)
	}
	req := srv.LastAtomic
	if req.OverwriteSoa == nil || *req.OverwriteSoa || req.OverwriteZoneApexNs == nil || *req.OverwriteZoneApexNs {
		t.Errorf("overwrite flags must be false: %v %v", req.OverwriteSoa, req.OverwriteZoneApexNs)
	}
	txt := srv.Zone("z1").Find(testFQDN, "TXT")
	if txt == nil || !reflect.DeepEqual(txt.Rdata, []string{`"KEY"`}) {
		t.Fatalf("challenge record = %+v", txt)
	}
	// FR-032: 新規作成時のみ TTL を定める。
	if txt.TTL == nil || *txt.TTL != dpf.ChallengeTTL {
		t.Errorf("TTL = %v, want %d", txt.TTL, dpf.ChallengeTTL)
	}
	if txt.Labels[challenge.LabelKey(challenge.TargetID("KEY"))] == "" {
		t.Errorf("expiry label missing: %v", txt.Labels)
	}
	// FR-064: 反映の Description。
	if req.Description == nil || *req.Description != "by cert-manager("+testOwner+")" {
		t.Errorf("Description = %v", req.Description)
	}
}

// FR-032: 既存のレコードに追記する場合、TTL とコメントを変更しない。
func TestApplyAppendKeepsTTL(t *testing.T) {
	z := applyZone()
	z.Records = append(z.Records, &dpftest.Record{Name: testFQDN, TTL: i32(300), Rrtype: "TXT",
		Rdata: []string{`"other"`}, Labels: map[string]string{"foreign": "x"}, Description: "keep"})
	srv := dpftest.New(t, z)
	c := newClient(t, srv)
	zone, fqdn := target()
	if _, err := c.Apply(context.Background(), zone, fqdn, opts(), noRecheck, presentPlan("KEY")); err != nil {
		t.Fatal(err)
	}
	srv.Lock()
	defer srv.Unlock()
	txt := srv.Zone("z1").Find(testFQDN, "TXT")
	if txt.TTL == nil || *txt.TTL != 300 || txt.Description != "keep" || txt.Labels["foreign"] != "x" {
		t.Fatalf("record = %+v", txt)
	}
	if !reflect.DeepEqual(txt.Rdata, []string{`"other"`, `"KEY"`}) {
		t.Errorf("Rdata = %v", txt.Rdata)
	}
}

// FR-035: RDATA が空になる計画ではレコードごと削除する。
func TestApplyDeletesRecord(t *testing.T) {
	z := applyZone()
	z.Records = append(z.Records, &dpftest.Record{Name: testFQDN, Rrtype: "TXT", Rdata: []string{`"OLD"`},
		Labels: map[string]string{challenge.LabelKey(challenge.TargetID("OLD")): "1"}})
	srv := dpftest.New(t, z)
	c := newClient(t, srv)
	zone, fqdn := target()
	plan := func(cur *challenge.Record) (challenge.Result, error) {
		return challenge.Plan(cur, challenge.Request{Action: challenge.CleanUp, Key: "NEW",
			Now: time.Now(), Grace: time.Hour, Threshold: 30 * time.Minute})
	}
	if _, err := c.Apply(context.Background(), zone, fqdn, opts(), noRecheck, plan); err != nil {
		t.Fatal(err)
	}
	srv.Lock()
	defer srv.Unlock()
	if srv.Zone("z1").Find(testFQDN, "TXT") != nil {
		t.Fatalf("challenge record remains")
	}
	if srv.Zone("z1").Find(testFQDN, "CAA") == nil {
		t.Fatalf("non-TXT record at the same name was removed")
	}
}

// FR-084a: 排他の内側で読んだ結果、変更が不要なら反映しない。
func TestApplySkipsWhenNoChange(t *testing.T) {
	srv := dpftest.New(t, applyZone())
	c := newClient(t, srv)
	zone, fqdn := target()
	var seen *challenge.Record
	plan := func(cur *challenge.Record) (challenge.Result, error) {
		seen = cur
		return challenge.Result{Changed: false, Record: cur}, nil
	}
	applied, err := c.Apply(context.Background(), zone, fqdn, opts(), noRecheck, plan)
	if err != nil || applied {
		t.Fatalf("Apply = %v, %v", applied, err)
	}
	if seen != nil {
		t.Errorf("planner saw %+v for an absent record", seen)
	}
	srv.Lock()
	defer srv.Unlock()
	if srv.Atomics != 0 {
		t.Fatalf("atomic changes were sent: %d", srv.Atomics)
	}
	// 排他は取得・解放された (既定の排他を用いている。D-16)。
	if _, ok := srv.Zone("z1").Labels["lock.dpf-go"]; !ok {
		t.Errorf("lock label was not written: %v", srv.Zone("z1").Labels)
	}
}

// 打ち切られた context では反映しない。
func TestApplyCanceledContext(t *testing.T) {
	srv := dpftest.New(t, applyZone())
	c := newClient(t, srv)
	zone, fqdn := target()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Apply(ctx, zone, fqdn, opts(), noRecheck, presentPlan("KEY"))
	if !errors.Is(err, dpf.ErrDeadline) {
		t.Fatalf("err = %v, want ErrDeadline", err)
	}
	srv.Lock()
	defer srv.Unlock()
	if srv.Atomics != 0 {
		t.Fatalf("atomic changes were sent after cancel")
	}
}

// FR-059: 反映の後に解放だけが失敗した場合も失敗として返す。
func TestApplyReleaseFailure(t *testing.T) {
	srv := dpftest.New(t, applyZone())
	srv.FailLabelPutAfterAtomic = true
	c := newClient(t, srv)
	zone, fqdn := target()
	_, err := c.Apply(context.Background(), zone, fqdn, opts(), noRecheck, presentPlan("KEY"))
	if err == nil {
		t.Fatal("release failure was not reported")
	}
	srv.Lock()
	defer srv.Unlock()
	if srv.Atomics != 1 {
		t.Fatalf("Atomics = %d", srv.Atomics)
	}
}

// FR-018: 書き込む直前に domainFilters を再判定し、不許可なら書き込まない。
func TestApplyRechecksFilter(t *testing.T) {
	srv := dpftest.New(t, applyZone())
	c := newClient(t, srv)
	zone, fqdn := target()
	denied := errors.New("denied")
	_, err := c.Apply(context.Background(), zone, fqdn, opts(), func() error { return denied }, presentPlan("KEY"))
	if !errors.Is(err, denied) {
		t.Fatalf("err = %v", err)
	}
	srv.Lock()
	defer srv.Unlock()
	if srv.Atomics != 0 {
		t.Fatalf("atomic changes were sent despite the recheck failure")
	}
}

// 計画の失敗 (ラベルの上限など) は反映せずに返す。
func TestApplyPlanError(t *testing.T) {
	srv := dpftest.New(t, applyZone())
	c := newClient(t, srv)
	zone, fqdn := target()
	_, err := c.Apply(context.Background(), zone, fqdn, opts(), noRecheck,
		func(*challenge.Record) (challenge.Result, error) { return challenge.Result{}, challenge.ErrLabelLimit })
	if !errors.Is(err, challenge.ErrLabelLimit) {
		t.Fatalf("err = %v", err)
	}
}

// D-15: 排他の保持者はロックオーナー、保持期間は 2 分 (既定)。
func TestApplyLockConfiguration(t *testing.T) {
	srv := dpftest.New(t, applyZone())
	var lockValue string
	srv.BeforeAtomic = func(s *dpftest.Server) { lockValue = s.Zone("z1").Labels["lock.dpf-go"] }
	c := newClient(t, srv)
	zone, fqdn := target()
	start := time.Now()
	if _, err := c.Apply(context.Background(), zone, fqdn, opts(), noRecheck, presentPlan("KEY")); err != nil {
		t.Fatal(err)
	}
	owner, deadline, ok := parseLock(lockValue)
	if !ok || owner != testOwner {
		t.Fatalf("lock value = %q", lockValue)
	}
	ttl := time.Unix(deadline, 0).Sub(start)
	if ttl < dpf.DefaultLockTTL-5*time.Second || ttl > dpf.DefaultLockTTL+5*time.Second {
		t.Errorf("lock TTL = %v, want about %v", ttl, dpf.DefaultLockTTL)
	}
}

// FR-048: 32 文字を超えるロックオーナーは書き込みを試みずに失敗する。
func TestApplyOwnerTooLong(t *testing.T) {
	srv := dpftest.New(t, applyZone())
	c := newClient(t, srv)
	zone, fqdn := target()
	o := opts()
	o.Owner = strings.Repeat("a", 33)
	_, err := c.Apply(context.Background(), zone, fqdn, o, noRecheck, presentPlan("KEY"))
	if !errors.Is(err, dpf.ErrOwnerTooLong) || !dpf.IsPermanent(err) {
		t.Fatalf("err = %v", err)
	}
}

// FR-064 / SC-014: ロックオーナーが 32 文字のとき Description は 49 文字で、切り詰めない。
func TestDescription(t *testing.T) {
	owner := strings.Repeat("o", 32)
	d := dpf.Description(owner)
	if d != "by cert-manager("+owner+")" || len(d) != 49 {
		t.Fatalf("Description = %q (%d)", d, len(d))
	}
}

func TestApplyDefaults(t *testing.T) {
	if dpf.DefaultLockTTL != 2*time.Minute || dpf.DefaultLockWait != 5*time.Second || dpf.ChallengeTTL != 60 {
		t.Fatalf("defaults = %v %v %v", dpf.DefaultLockTTL, dpf.DefaultLockWait, dpf.ChallengeTTL)
	}
}

// parseLock は dpf-go のゾーンのラベルの値 (<保持者>-<10 桁>) を読む。
func parseLock(v string) (string, int64, bool) {
	if len(v) < 12 || v[len(v)-11] != '-' {
		return "", 0, false
	}
	n, err := strconv.ParseInt(v[len(v)-10:], 10, 64)
	if err != nil {
		return "", 0, false
	}
	return v[:len(v)-11], n, true
}
