// SPDX-License-Identifier: Apache-2.0

package dpf_test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"

	sdk "github.com/iij/dpf-go"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
)

func init() {
	// 非同期ジョブの完了待ちを短くする。
	sdk.SyncWaitPollInterval = 0
}

// staticTokens は呼び出しのたびに異なるトークンを返す TokenProviders である。
type staticTokens struct {
	mu    sync.Mutex
	calls map[string]int
}

func (s *staticTokens) Provider(namespace, name, key string) (func(context.Context) (string, error), error) {
	src := namespace + "/" + name + "/" + key
	return func(context.Context) (string, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.calls == nil {
			s.calls = map[string]int{}
		}
		s.calls[src]++
		return fmt.Sprintf("token-%s-%d", name, s.calls[src]), nil
	}, nil
}

func newClient(t *testing.T, srv *dpftest.Server) *dpf.Client {
	t.Helper()
	clients := dpf.NewClients(dpf.NewSharedTransport(http.DefaultTransport,
		dpf.DefaultRateLimit, dpf.DefaultBurst, dpf.DefaultMaxConcurrency), &staticTokens{})
	c, err := clients.Get(dpf.Source{Endpoint: srv.URL, Namespace: "ns", SecretName: "dpf-token", SecretKey: "token"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return c
}
