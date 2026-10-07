// SPDX-License-Identifier: Apache-2.0

package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/version"
)

// OTLP の方式。OpenTelemetry の環境変数の値に従う。
const (
	ProtocolGRPC = "grpc"
	ProtocolHTTP = "http/protobuf"
)

// 信号の名前。
const (
	SignalTraces  = "traces"
	SignalMetrics = "metrics"
	SignalLogs    = "logs"
)

// Telemetry はメトリクス・トレース・ログの出力先である。
type Telemetry struct {
	// Registry は /metrics で公開する Prometheus 形式の計測である。常に有効。
	Registry *prometheus.Registry
	// MeterProvider は Prometheus 形式と (有効なら) OTLP の双方へ同じ計測を出す。
	MeterProvider *sdkmetric.MeterProvider
	// TracerProvider は (有効なら) OTLP へトレースを出す。無効でも span は作れる。
	TracerProvider *sdktrace.TracerProvider
	// LogHandler は OTLP へログを出す slog.Handler である。無効なら nil。
	// NewLogger の extra に渡す。標準出力への出力はこれと独立に常に行う。
	LogHandler slog.Handler
	// Exports は有効にした信号と方式である。OTLP を使わなければ空。
	Exports map[string]string

	loggerProvider *sdklog.LoggerProvider
	errLog         *slog.Logger
}

// Setup は OpenTelemetry の環境変数 (OTEL_EXPORTER_OTLP_ENDPOINT / _PROTOCOL と、
// 信号ごとの OTEL_EXPORTER_OTLP_<TRACES|METRICS|LOGS>_ENDPOINT / _PROTOCOL) から出力先を作る。
//
//   - OTLP 送出は既定で無効であり、送出先が設定された信号のみ有効にする (原則 V / VI)
//   - 方式は grpc と http/protobuf。省略時は http/protobuf。それ以外は失敗とする
//   - TLS 証明書の検証は exporter の既定 (有効) に従う。CA は OTEL_EXPORTER_OTLP_CERTIFICATE で与える
//   - 送出の失敗は errLog に記録し、本来の処理を止めない。errLog は標準出力のみへ出すロガーを渡す
func Setup(ctx context.Context, errLog *slog.Logger) (*Telemetry, error) {
	t := &Telemetry{Registry: prometheus.NewRegistry(), Exports: map[string]string{}, errLog: errLog}
	for _, sig := range []string{SignalTraces, SignalMetrics, SignalLogs} {
		proto, enabled, err := protocolOf(sig)
		if err != nil {
			return nil, err
		}
		if enabled {
			t.Exports[sig] = proto
		}
	}
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		errLog.Warn("telemetry export failed", "error", err)
	}))

	res := resource.NewSchemaless(
		attribute.String("service.name", "cert-manager-iij-dpf-webhook"),
		attribute.String("service.version", version.String()),
	)

	topts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	switch t.Exports[SignalTraces] {
	case ProtocolGRPC:
		exp, err := otlptracegrpc.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("otlp traces: %w", err)
		}
		topts = append(topts, sdktrace.WithBatcher(exp))
	case ProtocolHTTP:
		exp, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("otlp traces: %w", err)
		}
		topts = append(topts, sdktrace.WithBatcher(exp))
	}
	t.TracerProvider = sdktrace.NewTracerProvider(topts...)

	var readers []sdkmetric.Reader
	switch t.Exports[SignalMetrics] {
	case ProtocolGRPC:
		exp, err := otlpmetricgrpc.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("otlp metrics: %w", err)
		}
		readers = append(readers, sdkmetric.NewPeriodicReader(exp))
	case ProtocolHTTP:
		exp, err := otlpmetrichttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("otlp metrics: %w", err)
		}
		readers = append(readers, sdkmetric.NewPeriodicReader(exp))
	}
	mp, err := NewMeterProvider(t.Registry, readers...)
	if err != nil {
		return nil, err
	}
	t.MeterProvider = mp

	var lexp sdklog.Exporter
	switch t.Exports[SignalLogs] {
	case ProtocolGRPC:
		lexp, err = otlploggrpc.New(ctx)
	case ProtocolHTTP:
		lexp, err = otlploghttp.New(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("otlp logs: %w", err)
	}
	if lexp != nil {
		t.loggerProvider = sdklog.NewLoggerProvider(sdklog.WithResource(res),
			sdklog.WithProcessor(sdklog.NewBatchProcessor(lexp)))
		t.LogHandler = otelslog.NewHandler(InstrumentationName, otelslog.WithLoggerProvider(t.loggerProvider))
	}
	return t, nil
}

// protocolOf は信号の方式と、送出先が設定されているかを返す。
func protocolOf(signal string) (string, bool, error) {
	sig := map[string]string{SignalTraces: "TRACES", SignalMetrics: "METRICS", SignalLogs: "LOGS"}[signal]
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_" + sig + "_ENDPOINT")
	if endpoint == "" {
		endpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	}
	proto := os.Getenv("OTEL_EXPORTER_OTLP_" + sig + "_PROTOCOL")
	if proto == "" {
		proto = os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	}
	if proto == "" {
		proto = ProtocolHTTP
	}
	if endpoint == "" {
		return "", false, nil
	}
	if proto != ProtocolGRPC && proto != ProtocolHTTP {
		return "", false, fmt.Errorf("unsupported OTLP protocol for %s: %q (want %q or %q)",
			signal, proto, ProtocolGRPC, ProtocolHTTP)
	}
	return proto, true, nil
}

// ForceFlush は溜めているテレメトリを送出する。失敗は記録して返す。
func (t *Telemetry) ForceFlush(ctx context.Context) error {
	errs := []error{t.TracerProvider.ForceFlush(ctx), t.MeterProvider.ForceFlush(ctx)}
	if t.loggerProvider != nil {
		errs = append(errs, t.loggerProvider.ForceFlush(ctx))
	}
	err := errors.Join(errs...)
	if err != nil {
		t.errLog.Warn("telemetry export failed", "error", err)
	}
	return err
}

// Shutdown は送出を止める。
func (t *Telemetry) Shutdown(ctx context.Context) error {
	errs := []error{t.TracerProvider.Shutdown(ctx), t.MeterProvider.Shutdown(ctx)}
	if t.loggerProvider != nil {
		errs = append(errs, t.loggerProvider.Shutdown(ctx))
	}
	return errors.Join(errs...)
}
