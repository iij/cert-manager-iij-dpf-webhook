// SPDX-License-Identifier: Apache-2.0

package observability_test

import (
	"context"
	"strings"
	"testing"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/challenge"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/observability"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/solver"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/testutil"
)

// FR-091 / FR-092: 要求の受信から DPF 呼び出しまでを 1 つのトレースとして追跡できる。
// ゾーン名・レコード名・レコード値をトレースの属性に含めない。
func TestSolverTrace(t *testing.T) {
	spans := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans))
	m, _, _ := setupMetrics(t)
	srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
	s := testutil.NewSolverWith(t, srv, testutil.FixedToken("token"), (&testutil.LogBuffer{}).Logger(), nil,
		solver.WithTelemetry(tp, m))
	const key = "KEY-TRACE"
	if err := s.Present(testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge.example.jp.", key,
		testutil.Config(srv, []string{"example.jp"}, nil))); err != nil {
		t.Fatal(err)
	}
	_ = tp.ForceFlush(context.Background())

	got := spans.GetSpans()
	var root *tracetest.SpanStub
	for i := range got {
		if got[i].Name == observability.SpanPresent {
			root = &got[i]
		}
	}
	if root == nil {
		t.Fatalf("no %q span in %d spans", observability.SpanPresent, len(got))
	}
	if root.Parent.IsValid() {
		t.Errorf("the request span has a parent")
	}
	dpfCalls := 0
	for _, sp := range got {
		if sp.SpanContext.TraceID() != root.SpanContext.TraceID() {
			t.Errorf("span %q is in another trace", sp.Name)
		}
		if strings.HasPrefix(sp.Name, observability.SpanDPFPrefix) {
			dpfCalls++
		}
		text := spanText(sp)
		for _, bad := range []string{"example.jp", "_acme-challenge", key, challenge.QuoteTXT(key),
			challenge.TargetID(key)} {
			if strings.Contains(text, bad) {
				t.Errorf("span %q contains %q: %s", sp.Name, bad, text)
			}
		}
	}
	// 一覧・読み出し・ロック・一括置換・解放を含め、DPF の呼び出しが同じトレースに入る。
	if dpfCalls < 4 {
		t.Errorf("DPF call spans = %d", dpfCalls)
	}
}

// spanText は span の名前・属性・イベント・状態を 1 つの文字列にする。
func spanText(sp tracetest.SpanStub) string {
	var b strings.Builder
	b.WriteString(sp.Name + " " + sp.Status.Description)
	for _, a := range sp.Attributes {
		b.WriteString(" " + string(a.Key) + "=" + a.Value.String())
	}
	for _, e := range sp.Events {
		b.WriteString(" " + e.Name)
		for _, a := range e.Attributes {
			b.WriteString(" " + string(a.Key) + "=" + a.Value.String())
		}
	}
	return b.String()
}
