// SPDX-License-Identifier: Apache-2.0

package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// D-07 / FR-093: healthz・readyz・metrics のみを返す。
func TestExposedHandler(t *testing.T) {
	ready := false
	h := NewExposedHandler(func() bool { return ready }, prometheus.NewRegistry())
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	if c := get("/healthz").Code; c != http.StatusOK {
		t.Errorf("/healthz = %d", c)
	}
	if c := get("/readyz").Code; c != http.StatusServiceUnavailable {
		t.Errorf("/readyz before ready = %d", c)
	}
	ready = true
	if c := get("/readyz").Code; c != http.StatusOK {
		t.Errorf("/readyz = %d", c)
	}
	if rec := get("/metrics"); rec.Code != http.StatusOK {
		t.Errorf("/metrics = %d", rec.Code)
	}
	for _, p := range []string{"/", "/apis", "/debug/pprof/", "/config"} {
		if c := get(p).Code; c != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", p, c)
		}
	}
	if rec := get("/healthz"); strings.Contains(rec.Body.String(), "example") {
		t.Errorf("healthz leaks configuration")
	}
}

// exposed のポートは webhook のポートと同一にしない。
func TestPortsDiffer(t *testing.T) {
	if ExposedPort == WebhookPort || ExposedPort != 8080 || WebhookPort != 443 {
		t.Fatalf("ports = %d / %d", ExposedPort, WebhookPort)
	}
}
