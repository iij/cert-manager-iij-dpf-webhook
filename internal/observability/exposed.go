// SPDX-License-Identifier: Apache-2.0

package observability

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// WebhookPort は webhook (集約 API サーバ) の待ち受けポートである。
	WebhookPort = 443
	// ExposedPort は healthz と metrics の待ち受けポートである。webhook と同一にしない (D-07)。
	ExposedPort = 8080
)

// NewExposedHandler は /healthz・/readyz・/metrics のみを返すハンドラを返す。
// それ以外のパスには 404 を返し、構成を推測できる情報を返さない。
func NewExposedHandler(ready func() bool, gatherer prometheus.Gatherer) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !ready() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("GET /metrics", promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{}))
	return mux
}

// ServeExposed は ExposedPort で h を提供する。ctx が終わると停止する。
func ServeExposed(ctx context.Context, h http.Handler) error {
	srv := &http.Server{
		Addr:              net.JoinHostPort("", strconv.Itoa(ExposedPort)),
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
