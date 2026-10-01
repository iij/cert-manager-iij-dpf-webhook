// SPDX-License-Identifier: Apache-2.0

package dpf_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
)

// D-09: 複数のクライアントが 1 つのレート制限を共有する。
func TestSharedTransportRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	rt := dpf.NewSharedTransport(http.DefaultTransport, 20, 1, 100)
	a := &http.Client{Transport: rt}
	b := &http.Client{Transport: rt}

	start := time.Now()
	var wg sync.WaitGroup
	for _, c := range []*http.Client{a, b} {
		wg.Add(1)
		go func(c *http.Client) {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				resp, err := c.Get(srv.URL)
				if err != nil {
					t.Error(err)
					return
				}
				_ = resp.Body.Close()
			}
		}(c)
	}
	wg.Wait()
	// 10 件を 20 req/s (burst 1) で送ると、少なくとも 9 間隔 = 450ms かかる。
	// クライアントごとに制限が分かれていれば 4 間隔 = 200ms で終わる。
	if elapsed := time.Since(start); elapsed < 400*time.Millisecond {
		t.Fatalf("rate limit is not shared: elapsed %v", elapsed)
	}
}

// D-09: 複数のクライアントが 1 つの同時実行数の上限を共有する。
func TestSharedTransportConcurrency(t *testing.T) {
	var inflight, peak int64
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		n := atomic.AddInt64(&inflight, 1)
		for {
			p := atomic.LoadInt64(&peak)
			if n <= p || atomic.CompareAndSwapInt64(&peak, p, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt64(&inflight, -1)
	}))
	defer srv.Close()

	rt := dpf.NewSharedTransport(http.DefaultTransport, 1000, 1000, 2)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		c := &http.Client{Transport: rt}
		for j := 0; j < 3; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, err := c.Get(srv.URL)
				if err == nil {
					_ = resp.Body.Close()
				}
			}()
		}
	}
	wg.Wait()
	if peak > 2 {
		t.Fatalf("peak concurrency = %d, want <= 2", peak)
	}
}

func TestDefaults(t *testing.T) {
	if dpf.DefaultRateLimit != 5 || dpf.DefaultBurst != 10 || dpf.DefaultMaxConcurrency != 5 {
		t.Fatalf("defaults = %v/%v/%v", dpf.DefaultRateLimit, dpf.DefaultBurst, dpf.DefaultMaxConcurrency)
	}
}
