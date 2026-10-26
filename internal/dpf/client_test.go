// SPDX-License-Identifier: Apache-2.0

package dpf_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
)

type countingRT struct{ n int64 }

func (c *countingRT) RoundTrip(r *http.Request) (*http.Response, error) {
	atomic.AddInt64(&c.n, 1)
	return http.DefaultTransport.RoundTrip(r)
}

// D-09: 出所が同じなら同じクライアントを、異なれば別のクライアントを返す。
// いずれも共通の RoundTripper を用いる。
func TestClientsCache(t *testing.T) {
	srv := dpftest.New(t, dpftest.StandardZone("zone0000000001", "example.jp."))
	base := &countingRT{}
	clients := dpf.NewClients(dpf.NewSharedTransport(base, 1000, 1000, 10), &staticTokens{})

	src := dpf.Source{Endpoint: srv.URL, Namespace: "a", SecretName: "dpf-token", SecretKey: "token"}
	c1, err := clients.Get(src)
	if err != nil {
		t.Fatal(err)
	}
	c2, _ := clients.Get(src)
	if c1 != c2 {
		t.Errorf("same source returned different clients")
	}
	other := src
	other.Namespace = "b"
	c3, err := clients.Get(other)
	if err != nil {
		t.Fatal(err)
	}
	if c1 == c3 {
		t.Errorf("different sources returned the same client")
	}

	before := atomic.LoadInt64(&base.n)
	ctx := context.Background()
	zone := dnsname.MustParse("example.jp")
	if _, err := c1.FindZone(ctx, zone, zone); err != nil {
		t.Fatal(err)
	}
	if _, err := c3.FindZone(ctx, zone, zone); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt64(&base.n) < before+2 {
		t.Errorf("requests did not go through the shared transport")
	}
}

// FR-011: トークンは要求ごとに評価される (ローテーションに追随する)。
func TestTokenEvaluatedPerRequest(t *testing.T) {
	srv := dpftest.New(t, dpftest.StandardZone("zone0000000001", "example.jp."))
	c := newClient(t, srv)
	zone := dnsname.MustParse("example.jp")
	for i := 0; i < 2; i++ {
		if _, err := c.FindZone(context.Background(), zone, zone); err != nil {
			t.Fatal(err)
		}
	}
	srv.Lock()
	defer srv.Unlock()
	if n := len(srv.Tokens); n < 2 || srv.Tokens[n-1] == srv.Tokens[n-2] {
		t.Fatalf("token was not re-evaluated: %v", srv.Tokens)
	}
}
