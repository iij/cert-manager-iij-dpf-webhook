// SPDX-License-Identifier: Apache-2.0

package observability

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// InstrumentationName は本サービスの計測の名前である。
const InstrumentationName = "github.com/iij/cert-manager-iij-dpf-webhook"

// 計測の名前。Prometheus 形式の名前は OpenTelemetry の名前から機械的に導かれる (FR-090)。
const (
	// MetricRequests は Present / CleanUp の要求の件数である。
	MetricRequests = "dpf_webhook.challenge.requests"
	// MetricRecordChanges は DNS レコードの変更操作 (ゾーンへの反映を要したもの) の件数である。
	MetricRecordChanges = "dpf_webhook.dns.record_changes"

	PrometheusRequests      = "dpf_webhook_challenge_requests_total"
	PrometheusRecordChanges = "dpf_webhook_dns_record_changes_total"
)

// Metrics は本サービスの計測である。属性は action (present / cleanup) と result
// (success / failure) に限り、ゾーン名・レコード名・レコード値を含めない (FR-092)。
type Metrics struct {
	requests metric.Int64Counter
	changes  metric.Int64Counter
}

// NewMeterProvider は Prometheus 形式 (reg へ登録し /metrics で公開する) と、readers
// (OTLP など) の双方へ同じ計測を出す MeterProvider を返す。1 つの MeterProvider の計測を
// 読み手ごとに出力するため、両形式は同一の計測値を表す (原則 V)。
func NewMeterProvider(reg prometheus.Registerer, readers ...sdkmetric.Reader) (*sdkmetric.MeterProvider, error) {
	prom, err := otelprom.New(otelprom.WithRegisterer(reg), otelprom.WithoutTargetInfo(), otelprom.WithoutScopeInfo())
	if err != nil {
		return nil, err
	}
	opts := []sdkmetric.Option{sdkmetric.WithReader(prom)}
	for _, r := range readers {
		opts = append(opts, sdkmetric.WithReader(r))
	}
	return sdkmetric.NewMeterProvider(opts...), nil
}

// NewMetrics は mp に計測を作る。
func NewMetrics(mp metric.MeterProvider) (*Metrics, error) {
	m := mp.Meter(InstrumentationName)
	requests, err := m.Int64Counter(MetricRequests,
		metric.WithDescription("Number of Present / CleanUp requests by action and result."))
	if err != nil {
		return nil, err
	}
	changes, err := m.Int64Counter(MetricRecordChanges,
		metric.WithDescription("Number of DNS record change operations applied to DPF by action and result."))
	if err != nil {
		return nil, err
	}
	return &Metrics{requests: requests, changes: changes}, nil
}

// RecordRequest は要求 1 件の結果を数える。
func (m *Metrics) RecordRequest(ctx context.Context, action string, err error) {
	m.requests.Add(ctx, 1, metric.WithAttributes(attrs(action, err)...))
}

// RecordChange は DNS レコードの変更操作 1 件の結果を数える (FR-090)。
func (m *Metrics) RecordChange(ctx context.Context, action string, err error) {
	m.changes.Add(ctx, 1, metric.WithAttributes(attrs(action, err)...))
}

func attrs(action string, err error) []attribute.KeyValue {
	result := "success"
	if err != nil {
		result = "failure"
	}
	return []attribute.KeyValue{attribute.String("action", action), attribute.String("result", result)}
}
