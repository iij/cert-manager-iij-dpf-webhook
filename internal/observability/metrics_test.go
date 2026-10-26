// SPDX-License-Identifier: Apache-2.0

package observability_test

import (
	"context"
	"strings"
	"testing"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/observability"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/solver"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/testutil"
)

// otelValues は OpenTelemetry 形式で読んだ counter の値を、"action/result" をキーにして返す。
func otelValues(t *testing.T, r *sdkmetric.ManualReader, name string) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := r.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	out := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s is %T", name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				a, _ := dp.Attributes.Value(attribute.Key("action"))
				res, _ := dp.Attributes.Value(attribute.Key("result"))
				out[a.AsString()+"/"+res.AsString()] = dp.Value
			}
		}
	}
	return out
}

// promValues は Prometheus 形式で読んだ counter の値と、現れたラベル名を返す。
func promValues(t *testing.T, reg *prometheus.Registry, name string) (map[string]int64, map[string]bool) {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	out, labels := map[string]int64{}, map[string]bool{}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			var action, result string
			for _, l := range m.GetLabel() {
				labels[l.GetName()] = true
				switch l.GetName() {
				case "action":
					action = l.GetValue()
				case "result":
					result = l.GetValue()
				}
			}
			out[action+"/"+result] = int64(counterValue(m))
		}
	}
	return out, labels
}

func counterValue(m *dto.Metric) float64 { return m.GetCounter().GetValue() }

func setupMetrics(t *testing.T) (*observability.Metrics, *prometheus.Registry, *sdkmetric.ManualReader) {
	t.Helper()
	reg := prometheus.NewRegistry()
	reader := sdkmetric.NewManualReader()
	mp, err := observability.NewMeterProvider(reg, reader)
	if err != nil {
		t.Fatal(err)
	}
	m, err := observability.NewMetrics(mp)
	if err != nil {
		t.Fatal(err)
	}
	return m, reg, reader
}

// FR-090 / 原則 V: DNS レコードの変更操作の成否と件数を計測でき、Prometheus 形式と
// OpenTelemetry 形式が同一の計測値を表す。
func TestMetricsSameValuesInBothFormats(t *testing.T) {
	m, reg, reader := setupMetrics(t)
	ctx := context.Background()
	m.RecordChange(ctx, "present", nil)
	m.RecordChange(ctx, "present", nil)
	m.RecordChange(ctx, "cleanup", context.DeadlineExceeded)
	m.RecordRequest(ctx, "present", nil)

	want := map[string]int64{"present/success": 2, "cleanup/failure": 1}
	gotOtel := otelValues(t, reader, observability.MetricRecordChanges)
	gotProm, _ := promValues(t, reg, observability.PrometheusRecordChanges)
	for k, v := range want {
		if gotOtel[k] != v || gotProm[k] != v {
			t.Errorf("%s: otel = %d, prometheus = %d, want %d", k, gotOtel[k], gotProm[k], v)
		}
	}
	if len(gotOtel) != len(gotProm) {
		t.Errorf("series differ: otel %v, prometheus %v", gotOtel, gotProm)
	}
	reqOtel := otelValues(t, reader, observability.MetricRequests)
	reqProm, _ := promValues(t, reg, observability.PrometheusRequests)
	if reqOtel["present/success"] != 1 || reqProm["present/success"] != 1 {
		t.Errorf("requests: otel %v, prometheus %v", reqOtel, reqProm)
	}
}

// FR-090 / FR-092: solver の処理が計測され、ゾーン名・レコード名・レコード値がラベルに現れない。
func TestSolverMetrics(t *testing.T) {
	m, reg, reader := setupMetrics(t)
	srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
	s := testutil.NewSolverWith(t, srv, testutil.FixedToken("token"), (&testutil.LogBuffer{}).Logger(), nil,
		solver.WithTelemetry(noop.NewTracerProvider(), m))
	r := testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge.example.jp.", "KEY-METRICS",
		testutil.Config(srv, []string{"example.jp"}, nil))
	for i := 0; i < 2; i++ { // 2 回目は変更不要
		if err := s.Present(r); err != nil {
			t.Fatal(err)
		}
	}
	if got := otelValues(t, reader, observability.MetricRecordChanges); got["present/success"] != 1 || len(got) != 1 {
		t.Errorf("record changes = %v, want present/success = 1", got)
	}
	if got := otelValues(t, reader, observability.MetricRequests); got["present/success"] != 2 {
		t.Errorf("requests = %v, want present/success = 2", got)
	}

	_, labels := promValues(t, reg, observability.PrometheusRecordChanges)
	_, reqLabels := promValues(t, reg, observability.PrometheusRequests)
	for l := range reqLabels {
		labels[l] = true
	}
	for l := range labels {
		if l != "action" && l != "result" && l != "otel_scope_name" && l != "otel_scope_version" &&
			l != "otel_scope_schema_url" {
			t.Errorf("unexpected label %q", l)
		}
	}
	mfs, _ := reg.Gather()
	for _, mf := range mfs {
		for _, mm := range mf.GetMetric() {
			for _, l := range mm.GetLabel() {
				for _, bad := range []string{"example.jp", "_acme-challenge", "KEY-METRICS"} {
					if strings.Contains(l.GetValue(), bad) {
						t.Errorf("label %s=%q contains %q", l.GetName(), l.GetValue(), bad)
					}
				}
			}
		}
	}
}
