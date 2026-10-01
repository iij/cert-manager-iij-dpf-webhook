// SPDX-License-Identifier: Apache-2.0

// Package testutil は、solver を模擬 DPF に向けて組み立てるテスト用の補助である。
package testutil

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/solver"
)

// PodNamespace は本 Pod の namespace として与える値である。
const PodNamespace = "cert-manager"

// Tokens はトークンの出所を記録する TokenProviders である。
type Tokens struct {
	mu    sync.Mutex
	Calls []string // "<namespace>/<name>/<key>"
}

// Provider は呼び出しを記録し、固定のトークンを返す関数を返す。
func (t *Tokens) Provider(namespace, name, key string) (func(context.Context) (string, error), error) {
	t.mu.Lock()
	t.Calls = append(t.Calls, namespace+"/"+name+"/"+key)
	t.mu.Unlock()
	return func(context.Context) (string, error) { return "token", nil }, nil
}

// Sources は記録した出所を返す。
func (t *Tokens) Sources() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.Calls...)
}

// NewSolver は模擬 DPF に向けた Solver を返す。
func NewSolver(t testing.TB, srv *dpftest.Server, mutate func(*solver.Options)) (*solver.Solver, *Tokens) {
	t.Helper()
	o := solver.DefaultOptions()
	o.LockOwner = "cluster-a"
	o.PodNamespace = PodNamespace
	if mutate != nil {
		mutate(&o)
	}
	tokens := &Tokens{}
	s := solver.NewWithDependencies(o, slog.New(slog.NewTextHandler(io.Discard, nil)), tokens,
		dpf.NewSharedTransport(srv.Transport(), 1000, 1000, 10))
	return s, tokens
}

// Config は solver 設定の JSON を返す。endpoint は模擬 DPF を指す。
func Config(srv *dpftest.Server, filters []string, extra map[string]any) *apiextensionsv1.JSON {
	m := map[string]any{"endpoint": srv.URL, "domainFilters": filters}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return &apiextensionsv1.JSON{Raw: b}
}

// Request はチャレンジ要求を返す。既定は ClusterIssuer 由来である。
func Request(action whapi.ChallengeAction, zone, fqdn, key string, cfg *apiextensionsv1.JSON) *whapi.ChallengeRequest {
	return &whapi.ChallengeRequest{
		UID: "uid", Action: action, Type: "dns-01", DNSName: "example.jp", Key: key,
		ResourceNamespace: PodNamespace, ResolvedFQDN: fqdn, ResolvedZone: zone,
		AllowAmbientCredentials: true, Config: cfg,
	}
}
