// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"strings"
	"testing"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/testutil"
)

// FR-088: DPF のエラー応答を全文ログに記録する。長さで切り詰めず、問い合わせ用の識別子を失わない。
func TestDPFErrorResponseIsLoggedInFull(t *testing.T) {
	srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
	long := strings.Repeat("0123456789", 2000) // 20,000 文字
	srv.FailAtomic = true
	srv.ErrorBody = `{"request_id":"` + dpftest.RequestID + `","error_type":"ParameterError","error_message":"` +
		long + `","error_details":[{"code":"invalid","attribute":"records"}]}`

	var logs testutil.LogBuffer
	s := testutil.NewSolverWith(t, srv, testutil.FixedToken("token"), logs.Logger(), nil)
	err := s.Present(testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge.example.jp.", "KEY",
		testutil.Config(srv, []string{"example.jp"}, nil)))
	if err == nil {
		t.Fatal("Present succeeded")
	}
	out := logs.String()
	for _, want := range []string{dpftest.RequestID, long, "ParameterError", "error_details", "invalid"} {
		if !strings.Contains(out, want) {
			t.Errorf("log does not contain %.40q", want)
		}
	}
	if !strings.Contains(out, "status=400") {
		t.Errorf("log does not contain the status code")
	}
}
