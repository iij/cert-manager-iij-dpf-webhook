// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"context"
	"strings"
	"testing"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/observability"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/solver"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/testutil"
)

// FR-012: トークンの値は、ログ・エラーメッセージ・メトリクス・トレース・応答本文のいずれにも
// 現れない。DPF のエラー応答がトークンを含んでいても、その値のみを伏せ、それ以外は切り詰めずに
// 記録する (認証情報の非出力が全文記録に優先する)。
func TestTokenIsNeverOutput(t *testing.T) {
	srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
	long := strings.Repeat("abcdefghij", 500)
	srv.FailAtomic = true
	srv.ErrorBody = `{"request_id":"` + dpftest.RequestID + `","error_type":"Unauthorized","error_message":"token ` +
		distinctiveToken + ` rejected ` + long + `"}`

	reg := prometheus.NewRegistry()
	mp, err := observability.NewMeterProvider(reg)
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := observability.NewMetrics(mp)
	if err != nil {
		t.Fatal(err)
	}
	spans := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans))

	var logs testutil.LogBuffer
	s := testutil.NewSolverWith(t, srv, testutil.FixedToken(distinctiveToken), logs.Logger(), nil,
		solver.WithTelemetry(tp, metrics))
	perr := s.Present(testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge.example.jp.", "KEY",
		testutil.Config(srv, []string{"example.jp"}, nil)))
	if perr == nil {
		t.Fatal("Present succeeded")
	}
	_ = tp.ForceFlush(context.Background())

	if strings.Contains(perr.Error(), distinctiveToken) {
		t.Errorf("error message contains the token")
	}
	out := logs.String()
	if strings.Contains(out, distinctiveToken) {
		t.Errorf("log contains the token")
	}
	if !strings.Contains(out, "rejected "+long) || !strings.Contains(out, dpftest.RequestID) {
		t.Errorf("the rest of the error response is not logged in full")
	}

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	for _, mf := range mfs {
		_ = expfmt.NewEncoder(&buf, expfmt.NewFormat(expfmt.TypeTextPlain)).Encode(mf)
	}
	if strings.Contains(buf.String(), distinctiveToken) {
		t.Errorf("metrics contain the token")
	}
	if len(spans.GetSpans()) == 0 {
		t.Fatal("no spans")
	}
	for _, sp := range spans.GetSpans() {
		if strings.Contains(spanText(sp), distinctiveToken) {
			t.Errorf("span %q contains the token", sp.Name)
		}
	}
}

// spanText は span の名前・属性・イベント・状態を 1 つの文字列にする。
func spanText(sp tracetest.SpanStub) string {
	var b strings.Builder
	b.WriteString(sp.Name + " " + sp.Status.Description)
	for _, a := range sp.Attributes {
		b.WriteString(" " + string(a.Key) + "=" + a.Value.Emit())
	}
	for _, e := range sp.Events {
		b.WriteString(" " + e.Name)
		for _, a := range e.Attributes {
			b.WriteString(" " + string(a.Key) + "=" + a.Value.Emit())
		}
	}
	return b.String()
}
