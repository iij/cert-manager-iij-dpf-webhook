// SPDX-License-Identifier: Apache-2.0

// Package solver は cert-manager の webhook.Solver を実装する。cert-manager の契約に対する
// 薄い変換層であり、DPF 固有の処理は internal/dpf に、判断は internal/challenge に委ねる。
package solver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	restclient "k8s.io/client-go/rest"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
)

// SolverName は solver 名である (FR-001)。
const SolverName = "iij-dpf"

// errNotImplemented は US1 で実装する操作の仮置きである。
var errNotImplemented = errors.New("not implemented")

// Solver は webhook.Solver の実装である。
type Solver struct {
	opts      Options
	log       *slog.Logger
	transport http.RoundTripper
	clients   *dpf.Clients
	ready     atomic.Bool

	// defaultEndpoint は solver 設定の endpoint を省略したときの接続先である (テストで差し替える)。
	defaultEndpoint string
}

// New は Solver を作る。DPF クライアントは Initialize で Kubernetes の資格情報を得てから作る。
func New(o Options, log *slog.Logger) *Solver {
	return &Solver{
		opts:            o,
		log:             log,
		transport:       dpf.NewSharedTransport(nil, dpf.DefaultRateLimit, dpf.DefaultBurst, dpf.DefaultMaxConcurrency),
		defaultEndpoint: DefaultEndpoint,
	}
}

// NewWithDependencies は依存を与えて Solver を作る。Initialize を経ずに要求を処理できる。
func NewWithDependencies(o Options, log *slog.Logger, tokens dpf.TokenProviders, transport http.RoundTripper) *Solver {
	s := &Solver{
		opts:            o,
		log:             log,
		transport:       transport,
		clients:         dpf.NewClients(transport, tokens),
		defaultEndpoint: DefaultEndpoint,
	}
	s.ready.Store(true)
	return s
}

// Ready は要求を処理できる状態かを返す (readiness probe に用いる)。
func (s *Solver) Ready() bool { return s.ready.Load() }

// Name は solver 名を返す。
func (s *Solver) Name() string { return SolverName }

// Initialize は US1 で実装する。
func (s *Solver) Initialize(*restclient.Config, <-chan struct{}) error { return errNotImplemented }

// Present は US1 で実装する。
func (s *Solver) Present(*whapi.ChallengeRequest) error { return errNotImplemented }

// CleanUp は US1 で実装する。
func (s *Solver) CleanUp(*whapi.ChallengeRequest) error { return errNotImplemented }

// operationContext は操作期限の context を返す。
//
// Present / CleanUp には呼び出しの context が渡されないため、本サービス自身が期限を持つ。
// 要求の context から派生させない (FR-057a)。呼び出し元へタイムアウトが返ったのちも処理を
// 完了まで進め、再試行が望む最終状態を見つけて成功することで収束させる (research.md D-15)。
func operationContext(o Options) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), o.Deadline)
}
