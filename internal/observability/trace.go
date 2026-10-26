// SPDX-License-Identifier: Apache-2.0

package observability

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// span の名前。ゾーン名・レコード名・レコード値を名前と属性に含めない (FR-092)。
//
// Present / CleanUp には cert-manager の要求の context が渡されないため、要求の受信は
// solver が要求を受け取った時点の span (SpanPresent / SpanCleanUp) で表す。DPF の呼び出しは
// その子の span (SpanDPFPrefix + メソッド) になり、1 つのトレースとして追跡できる (FR-091)。
const (
	SpanPresent   = "iij-dpf.present"
	SpanCleanUp   = "iij-dpf.cleanup"
	SpanDPFPrefix = "dpf "
)

type tracerKey struct{}

// WithTracer は t を運ぶ context を返す。
//
// DPF の呼び出しの span は、context が運ぶトレーサで作る。dpf-go は大域のトレーサで HTTP の
// 呼び出しごとに span を作り、その属性 url.full に問い合わせ文字列 (ゾーン名・レコード名を含む)
// を載せる。FR-092 に反するため大域のトレーサは設定せず (dpf-go の span は記録されない)、
// 本サービスのトレーサを context で渡す。親子関係は span context によって保たれる。
func WithTracer(ctx context.Context, t trace.Tracer) context.Context {
	return context.WithValue(ctx, tracerKey{}, t)
}

// Tracer は ctx が運ぶトレーサを返す。無ければ記録しないトレーサを返す。
func Tracer(ctx context.Context) trace.Tracer {
	if t, ok := ctx.Value(tracerKey{}).(trace.Tracer); ok {
		return t
	}
	return noop.NewTracerProvider().Tracer(InstrumentationName)
}
