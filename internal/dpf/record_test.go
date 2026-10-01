// SPDX-License-Identifier: Apache-2.0

package dpf_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
)

// D-11 2a / FR-084: 排他を取らずに、対象名の TXT の RDATA とラベルを読む。
func TestReadChallenge(t *testing.T) {
	z := dpftest.StandardZone("z1", "example.jp.")
	z.Records = append(z.Records,
		&dpftest.Record{Name: "_acme-challenge.example.jp.", Rrtype: "TXT",
			Rdata: []string{`"a"`, `"b"`}, Labels: map[string]string{"k": "v"}},
		&dpftest.Record{Name: "_acme-challenge.example.jp.", Rrtype: "CNAME", Rdata: []string{"x.example."}},
		&dpftest.Record{Name: "other.example.jp.", Rrtype: "TXT", Rdata: []string{`"c"`}},
		// 追加予定のレコードは公開されていないため読まない。
		&dpftest.Record{Name: "pending.example.jp.", Rrtype: "TXT", Rdata: []string{`"p"`}, State: 1},
	)
	srv := dpftest.New(t, z)
	c := newClient(t, srv)
	zone := dpf.Zone{ID: "z1", Name: dnsname.MustParse("example.jp")}

	rec, err := c.ReadChallenge(context.Background(), zone, dnsname.MustParse("_ACME-challenge.example.jp"))
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil || !reflect.DeepEqual(rec.Values, []string{`"a"`, `"b"`}) || rec.Labels["k"] != "v" {
		t.Fatalf("record = %+v", rec)
	}

	none, err := c.ReadChallenge(context.Background(), zone, dnsname.MustParse("pending.example.jp"))
	if err != nil || none != nil {
		t.Fatalf("pending record was read: %+v, %v", none, err)
	}
	srv.Lock()
	defer srv.Unlock()
	if srv.ZoneLabelPuts != 0 {
		t.Errorf("reading acquired the lock")
	}
}
