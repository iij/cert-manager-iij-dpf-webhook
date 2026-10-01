// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"context"
	"net/http"
	"sync"
	"time"

	sdk "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/utils"
)

// requestTimeout は DPF API への 1 回の要求のタイムアウトである (dpf-go の既定と同じ)。
const requestTimeout = 30 * time.Second

// Source はトークンの出所と接続先である。クライアントのキャッシュのキーになる。
type Source struct {
	Endpoint   string
	Namespace  string
	SecretName string
	SecretKey  string
}

// TokenProviders はトークンの出所から、呼び出しのたびにトークンを返す関数を作る。
type TokenProviders interface {
	Provider(namespace, name, key string) (func(context.Context) (string, error), error)
}

// Clients はトークンの出所ごとに DPF クライアントを保持する (D-09)。
// レート制限と同時実行数は、共通の RoundTripper によってプロセス全体で 1 つにする。
type Clients struct {
	transport http.RoundTripper
	tokens    TokenProviders

	mu    sync.Mutex
	cache map[Source]*Client
}

// NewClients は Clients を返す。transport は NewSharedTransport で作ったものを渡す。
func NewClients(transport http.RoundTripper, tokens TokenProviders) *Clients {
	return &Clients{transport: transport, tokens: tokens, cache: map[Source]*Client{}}
}

// Client は 1 つのトークンの出所に対応する DPF クライアントである。
type Client struct {
	api *sdk.APIClient
}

// Get は出所に対応するクライアントを返す。同じ出所には同じクライアントを返す。
// 作成に失敗した場合はキャッシュしない (Secret の作成後に回復できるようにする)。
func (c *Clients) Get(src Source) (*Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cl, ok := c.cache[src]; ok {
		return cl, nil
	}
	p, err := c.tokens.Provider(src.Namespace, src.SecretName, src.SecretKey)
	if err != nil {
		return nil, Convert(err)
	}
	uc, err := utils.NewClient(
		utils.WithEndpoint(src.Endpoint),
		utils.WithTokenProvider(utils.TokenProvider(p)),
		utils.WithHTTPClient(&http.Client{Transport: c.transport, Timeout: requestTimeout}),
	)
	if err != nil {
		return nil, Convert(err)
	}
	cl := &Client{api: uc.GetAPIClient()}
	c.cache[src] = cl
	return cl, nil
}
