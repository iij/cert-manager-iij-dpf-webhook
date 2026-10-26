// SPDX-License-Identifier: Apache-2.0

package dpf_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	sdk "github.com/iij/dpf-go"
)

func contains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }

// newErrorServer は常に 400 と body を返すサーバへ向けた設定を返す。
func newErrorServer(t *testing.T, body string) *sdk.Configuration {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	cfg := sdk.NewConfiguration()
	cfg.Servers = sdk.ServerConfigurations{{URL: srv.URL}}
	return cfg
}
