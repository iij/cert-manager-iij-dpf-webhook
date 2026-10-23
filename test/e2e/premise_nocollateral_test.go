// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package e2e

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	sdk "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/utils"
)

// T015 (FR-055 / FR-056): 無関係なレコードの RDATA・TTL・ラベル・コメントと、SOA・Zone Apex の NS が
// 変わらない。
func TestPremiseNoCollateral(t *testing.T) {
	e := loadEnv(t)
	ctx := setupContext(t)
	name := "e2e-collateral." + e.zone.String()

	// TTL が null・ラベル付き・コメント付きの無関係なレコードを置く。
	ap := e.applier(utils.WithOwner("e2e-setup"))
	err := ap.Apply(ctx, func(_ context.Context, rs []sdk.OverwriteRecordsInner) ([]sdk.OverwriteRecordsInner, error) {
		out := rs[:0:0]
		for _, r := range rs {
			if r.Name != name {
				out = append(out, r)
			}
		}
		return append(out, sdk.OverwriteRecordsInner{
			Name: name, Ttl: *sdk.NewNullableInt32(nil), Rrtype: sdk.RECORDSRRTYPE_TXT,
			Rdata:       []sdk.RecordsRdataInner{{Value: sdk.PtrString(`"unrelated"`)}},
			Description: "unrelated", Labels: map[string]string{"owner": "someone"},
		}), nil
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	before := e.snapshot(t)

	s := e.newSolver(t, nil)
	key := "collateral-" + time.Now().UTC().Format("150405")
	if err := s.Present(e.request(whapi.ChallengeActionPresent, "_acme-challenge.e2e-collateral", key)); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if err := s.CleanUp(e.request(whapi.ChallengeActionCleanUp, "_acme-challenge.e2e-collateral", key)); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if after := e.snapshot(t); !reflect.DeepEqual(before, after) {
		t.Errorf("無関係なレコードが変わった:\n before %v\n after  %v", before, after)
	}
}

// snapshot はチャレンジレコード以外の公開レコードを比較できる形で返す。
func (e *env) snapshot(t *testing.T) map[string]string {
	t.Helper()
	list, _, err := e.api.RecordsAPI.GetRecordCurrents(context.Background(), e.zoneID).ExecuteAll()
	if err != nil {
		t.Fatalf("GetRecordCurrents: %v", err)
	}
	out := map[string]string{}
	for _, r := range list.Results {
		if len(r.Name) >= len("_acme-challenge") && r.Name[:len("_acme-challenge")] == "_acme-challenge" {
			continue
		}
		if r.Rrtype == sdk.RECORDSRRTYPE_SOA {
			r = withoutSerial(r)
		}
		b, _ := r.MarshalJSON()
		out[r.Name+"/"+string(r.Rrtype)] = string(b)
	}
	return out
}

// withoutSerial は SOA から、反映のたびに DPF が改める値 (シリアルとレコードの ID) を除く。
// DPF はゾーンへの反映ごとにシリアルを進め、SOA のレコードを作り直す。本サービスが SOA を
// 変えたのではないため、FR-056 の比較から外す。
func withoutSerial(r sdk.Record) sdk.Record {
	r.Id = ""
	rdata := make([]sdk.RecordsRdataInner, len(r.Rdata))
	for i, d := range r.Rdata {
		if d.Value != nil {
			f := strings.Fields(*d.Value) // domain-string: SOA の RDATA を欄に分ける
			if len(f) == 7 {
				f[2] = "-"
			}
			d.Value = sdk.PtrString(strings.Join(f, " ")) // domain-string: SOA の RDATA を組み直す
		}
		rdata[i] = d
	}
	r.Rdata = rdata
	return r
}
