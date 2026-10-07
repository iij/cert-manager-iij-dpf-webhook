// SPDX-License-Identifier: Apache-2.0

package observability_test

import (
	"context"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/observability"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/testutil"
)

// clearOTLPEnv は OTLP の設定を空にする。
func clearOTLPEnv(t *testing.T) {
	t.Helper()
	for _, sig := range []string{"", "TRACES_", "METRICS_", "LOGS_"} {
		t.Setenv("OTEL_EXPORTER_OTLP_"+sig+"ENDPOINT", "")
		t.Setenv("OTEL_EXPORTER_OTLP_"+sig+"PROTOCOL", "")
		t.Setenv("OTEL_EXPORTER_OTLP_"+sig+"CERTIFICATE", "")
		t.Setenv("OTEL_EXPORTER_OTLP_"+sig+"INSECURE", "")
	}
}

func setup(t *testing.T) (*observability.Telemetry, *testutil.LogBuffer) {
	t.Helper()
	var errLog testutil.LogBuffer
	tel, err := observability.Setup(context.Background(), errLog.Logger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tel.Shutdown(ctx)
	})
	return tel, &errLog
}

// 原則 V / VI: OTLP 送出は既定で無効であり、送出先が設定された場合にのみ有効になる。
func TestOTLPDisabledByDefault(t *testing.T) {
	clearOTLPEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc") // 送出先が無ければ有効にならない
	tel, _ := setup(t)
	if len(tel.Exports) != 0 || tel.LogHandler != nil {
		t.Fatalf("exports = %v, log handler = %v", tel.Exports, tel.LogHandler)
	}
	if tel.Registry == nil || tel.TracerProvider == nil || tel.MeterProvider == nil {
		t.Fatal("Prometheus registry, tracer and meter providers must exist without OTLP")
	}
}

// 送出先と方式 (gRPC / HTTP) は信号ごとに決まる。
func TestOTLPEnabledByEndpoint(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want map[string]string
	}{
		{"トレースのみ", map[string]string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "https://otel.example:4318/v1/traces"},
			map[string]string{"traces": "http/protobuf"}},
		{"共通の送出先と gRPC", map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "https://otel.example:4317",
			"OTEL_EXPORTER_OTLP_PROTOCOL": "grpc"},
			map[string]string{"traces": "grpc", "metrics": "grpc", "logs": "grpc"}},
		{"信号ごとの方式", map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "https://otel.example:4317",
			"OTEL_EXPORTER_OTLP_PROTOCOL": "grpc", "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL": "http/protobuf"},
			map[string]string{"traces": "grpc", "metrics": "http/protobuf", "logs": "grpc"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clearOTLPEnv(t)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			tel, _ := setup(t)
			if !reflect.DeepEqual(tel.Exports, c.want) {
				t.Fatalf("exports = %v, want %v", tel.Exports, c.want)
			}
			if (tel.LogHandler != nil) != (c.want["logs"] != "") {
				t.Fatalf("log handler = %v", tel.LogHandler)
			}
		})
	}
}

// 未知の方式は起動時の失敗とする (既定値へ読み替えない)。
func TestOTLPRejectsUnknownProtocol(t *testing.T) {
	clearOTLPEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://otel.example:4318")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/json")
	if _, err := observability.Setup(context.Background(), (&testutil.LogBuffer{}).Logger()); err == nil {
		t.Fatal("unknown protocol was accepted")
	}
}

// 原則 V: OTLP 送出先の TLS 証明書を既定で検証する。信頼できない証明書の送出先には送らず、
// 失敗をログに記録する。CA を与えれば送れる。
func TestOTLPVerifiesTLSByDefault(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	clearOTLPEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", srv.URL+"/v1/traces")
	tel, errLog := setup(t)
	_, span := tel.TracerProvider.Tracer("test").Start(context.Background(), "span")
	span.End()
	_ = tel.ForceFlush(context.Background())
	if received.Load() != 0 {
		t.Fatal("exported to an untrusted endpoint")
	}
	if !strings.Contains(errLog.String(), "telemetry export failed") {
		t.Fatalf("export failure is not logged: %s", errLog.String())
	}

	ca := filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(ca, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_CERTIFICATE", ca)
	tel2, _ := setup(t)
	_, span = tel2.TracerProvider.Tracer("test").Start(context.Background(), "span")
	span.End()
	_ = tel2.ForceFlush(context.Background())
	if received.Load() == 0 {
		t.Fatal("not exported to the endpoint trusted by the given CA")
	}
}

// 原則 V: 送出の失敗で本来の処理を止めない。標準出力のログは続き、失敗はログに記録される。
func TestOTLPFailureDoesNotStopProcessing(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close() // 接続できない送出先

	clearOTLPEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://"+addr)
	t.Setenv("OTEL_BSP_SCHEDULE_DELAY", "50")
	t.Setenv("OTEL_BLRP_SCHEDULE_DELAY", "50")
	tel, errLog := setup(t)

	var stdout testutil.LogBuffer
	log := observability.NewLogger(&stdout, 0, tel.LogHandler)
	start := time.Now()
	for i := 0; i < 1000; i++ {
		log.Info("dns change", "i", i)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("logging took %v with an unreachable OTLP endpoint", d)
	}
	if n := strings.Count(stdout.String(), "dns change"); n != 1000 {
		t.Fatalf("stdout has %d of 1000 records", n)
	}
	_, span := tel.TracerProvider.Tracer("test").Start(context.Background(), "span")
	span.End()

	// バックグラウンドの送出の失敗が記録される。
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(errLog.String(), "telemetry export failed") {
		if time.Now().After(deadline) {
			t.Fatal("background export failure is not logged")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
