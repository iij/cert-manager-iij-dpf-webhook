// SPDX-License-Identifier: Apache-2.0

package solver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	sdk "github.com/iij/dpf-go"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
)

func init() { sdk.SyncWaitPollInterval = 0 }

type fixedTokens struct{}

func (fixedTokens) Provider(_, _, _ string) (func(context.Context) (string, error), error) {
	return func(context.Context) (string, error) { return "token", nil }, nil
}

func testOptions() Options {
	o := DefaultOptions()
	o.LockOwner = "cluster-a"
	o.PodNamespace = "cert-manager"
	return o
}

// newTestSolver は模擬 DPF に向けた Solver を返す。endpoint は config で上書きする。
func newTestSolver(t *testing.T, srv *dpftest.Server, o Options) *Solver {
	t.Helper()
	s := NewWithDependencies(o, slog.New(slog.NewTextHandler(io.Discard, nil)), fixedTokens{},
		dpf.NewSharedTransport(http.DefaultTransport, 1000, 1000, 10))
	s.defaultEndpoint = srv.URL
	return s
}

func challengeRequest(zone, fqdn, key, cfg string) *whapi.ChallengeRequest {
	return &whapi.ChallengeRequest{
		UID: "uid", Action: whapi.ChallengeActionPresent, Type: "dns-01", DNSName: "example.jp",
		Key: key, ResourceNamespace: "cert-manager", ResolvedFQDN: fqdn, ResolvedZone: zone,
		AllowAmbientCredentials: true, Config: &apiextensionsv1.JSON{Raw: []byte(cfg)},
	}
}
