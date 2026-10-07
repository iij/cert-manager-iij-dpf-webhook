// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"net/http"

	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"
)

// DPF API への送出の既定の上限。プロセス全体で 1 つの制限として効かせる (D-09)。
const (
	DefaultRateLimit      rate.Limit = 5
	DefaultBurst          int        = 10
	DefaultMaxConcurrency int        = 5
)

// sharedTransport はレート制限と同時実行数の上限を、これを共有するすべてのクライアントに
// 対して 1 つの制限として適用する。
type sharedTransport struct {
	base    http.RoundTripper
	limiter *rate.Limiter
	sem     *semaphore.Weighted
}

// NewSharedTransport はプロセス全体で 1 つ作り、トークンの出所ごとのクライアントへ
// 共通に渡す RoundTripper を返す。クライアントのキャッシュごとに制限が分かれると、
// 相手のサービスに対する配慮として意味をなさないためである (D-09)。
func NewSharedTransport(base http.RoundTripper, limit rate.Limit, burst, maxConcurrency int) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &sharedTransport{
		base:    &observedTransport{base: base},
		limiter: rate.NewLimiter(limit, burst),
		sem:     semaphore.NewWeighted(int64(maxConcurrency)),
	}
}

func (t *sharedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	if err := t.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	if err := t.sem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer t.sem.Release(1)
	return t.base.RoundTrip(req)
}
