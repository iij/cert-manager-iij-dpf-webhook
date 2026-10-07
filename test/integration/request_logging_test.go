// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"strings"
	"testing"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/challenge"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/testutil"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/tokensource"
)

const distinctiveToken = "tok-5f1c0e7d-DO-NOT-LOG"

// zoneWithForeignRecords は本サービスと無関係なレコードを含むゾーンを返す。
func zoneWithForeignRecords() *dpftest.Zone {
	z := dpftest.StandardZone("zone0000000001", "example.jp.")
	ttl := int32(300)
	z.Records = append(z.Records,
		&dpftest.Record{Name: "www.example.jp.", TTL: &ttl, Rrtype: "A", Rdata: []string{"192.0.2.10"}},
		&dpftest.Record{Name: "example.jp.", TTL: &ttl, Rrtype: "TXT", Rdata: []string{`"v=spf1 -all-foreign"`}})
	return z
}

// FR-088a / FR-088b: DPF と Kubernetes API へ送った要求を記録する。一括置換は差分と総数のみとし、
// ゾーン全体のレコードを記録しない。トークンと認証ヘッダを含めない。記録を webhook の応答
// (Present の返すエラー。cert-manager はそれを HTTP 応答の Status.Message に載せる) に含めない。
func TestRequestsAreLogged(t *testing.T) {
	srv := dpftest.NewTLS(t, zoneWithForeignRecords())
	f := &fakeSecrets{}
	f.set("team-a", "dpf-token", "token", distinctiveToken)
	var logs testutil.LogBuffer
	s := testutil.NewSolverWith(t, srv, tokensource.New(f), logs.Logger(), nil)
	r := testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge.example.jp.", "KEY-REQLOG",
		testutil.Config(srv, []string{"example.jp"}, nil))
	r.AllowAmbientCredentials = false
	r.ResourceNamespace = "team-a"
	if err := s.Present(r); err != nil {
		t.Fatal(err)
	}
	out := logs.String()

	lines := func(substr ...string) []string {
		var got []string
	next:
		for _, l := range strings.Split(out, "\n") {
			for _, s := range substr {
				if !strings.Contains(l, s) {
					continue next
				}
			}
			got = append(got, l)
		}
		return got
	}

	// Kubernetes API: Secret の取得 (メソッドとパス)。
	if len(lines("method=GET", "path=/api/v1/namespaces/team-a/secrets/dpf-token")) == 0 {
		t.Errorf("Secret read is not logged")
	}
	// DPF: ゾーンの一覧、ゾーンのラベルの書き込み (排他。本文を切り詰めずに記録する)。
	if len(lines("method=GET", "path=/zones")) == 0 {
		t.Errorf("zone list request is not logged")
	}
	put := lines("method=PUT", "path=/zones/zone0000000001/labels", "zoneID=zone0000000001")
	if len(put) == 0 || !strings.Contains(strings.Join(put, "\n"), "lock.dpf-go") {
		t.Errorf("zone label request (with its body) is not logged:\n%s", strings.Join(put, "\n"))
	}
	// 一括置換: 本文の代わりに総数を記録する。
	atomic := lines("method=PATCH", "path=/zones/zone0000000001/atomic_changes")
	if len(atomic) == 0 || !strings.Contains(atomic[0], "records=") {
		t.Errorf("atomic request is not logged with the record count:\n%s", strings.Join(atomic, "\n"))
	}
	// 差分: レコード名・加えた RDATA・加えたラベル。
	change := lines("_acme-challenge.example.jp.", challenge.LabelKey(challenge.TargetID("KEY-REQLOG")))
	if len(change) == 0 || !strings.Contains(change[0], "KEY-REQLOG") {
		t.Errorf("the change diff is not logged")
	}
	// ゾーン全体のレコードを記録しない。
	for _, foreign := range []string{"192.0.2.10", "spf1 -all-foreign", "ns1.example."} {
		if strings.Contains(out, foreign) {
			t.Errorf("log contains a record outside the change: %q", foreign)
		}
	}
	// トークンと認証ヘッダを記録しない。
	if strings.Contains(out, distinctiveToken) || strings.Contains(out, "Bearer") || strings.Contains(out, "Authorization") {
		t.Errorf("log contains the token or the authorization header")
	}
}

// FR-088b: 失敗したときに返すエラー (webhook の応答本文になる) に、DPF のエラー応答の本文と
// 送った要求の内容を含めない。ログには残す。
func TestRecordsAreNotInWebhookResponse(t *testing.T) {
	srv := dpftest.NewTLS(t, zoneWithForeignRecords())
	srv.FailAtomic = true
	srv.ErrorBody = `{"request_id":"` + dpftest.RequestID + `","error_type":"ParameterError","error_message":"bad records"}`
	var logs testutil.LogBuffer
	s := testutil.NewSolverWith(t, srv, testutil.FixedToken(distinctiveToken), logs.Logger(), nil)
	err := s.Present(testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge.example.jp.", "KEY",
		testutil.Config(srv, []string{"example.jp"}, nil)))
	if err == nil {
		t.Fatal("Present succeeded")
	}
	for _, leaked := range []string{"bad records", dpftest.RequestID, "atomic_changes", "192.0.2.10", distinctiveToken} {
		if strings.Contains(err.Error(), leaked) {
			t.Errorf("returned error contains %q: %v", leaked, err)
		}
	}
	if !strings.Contains(logs.String(), "bad records") {
		t.Errorf("the error response is not logged")
	}
}
