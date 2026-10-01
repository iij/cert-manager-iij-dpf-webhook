// SPDX-License-Identifier: Apache-2.0

//go:build e2e

// Package e2e は実際の DPF と検証用ゾーンに対する検証である (憲章「実環境での検証」)。
// 検証用ゾーンを共有するため、並列に実行しないこと (go test -p 1)。
//
// 必要な環境変数:
//
//	DPF_API_TOKEN    検証用の契約のトークン
//	DPF_TEST_ZONE    破壊的操作を行ってよいゾーン (本番ゾーンを使わない)
//	DPF_API_ENDPOINT 省略時は本番の API エンドポイント
//	DPF_API_TOKEN_2  別ユーザのトークン (別トークン間の排他の確認に用いる。省略時はその確認を飛ばす)
package e2e

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	sdk "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/utils"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/solver"
)

type env struct {
	token, token2, endpoint string
	zone                    dnsname.Name
	api                     *sdk.APIClient
	zoneID                  string
}

func loadEnv(t *testing.T) *env {
	t.Helper()
	e := &env{token: os.Getenv("DPF_API_TOKEN"), token2: os.Getenv("DPF_API_TOKEN_2"),
		endpoint: os.Getenv("DPF_API_ENDPOINT")}
	zone := os.Getenv("DPF_TEST_ZONE")
	if e.token == "" || zone == "" {
		t.Skip("DPF_API_TOKEN と DPF_TEST_ZONE が必要")
	}
	e.zone = dnsname.MustParse(zone)
	e.api = e.client(t, e.token)
	z, err := utils.GetZoneFromName(context.Background(), e.api.ZonesAPI, e.zone.String(), false)
	if err != nil || !dnsname.MustParse(z.Name).Equal(e.zone) {
		t.Fatalf("検証用ゾーン %s が見つからない: %v", e.zone, err)
	}
	e.zoneID = z.Id
	return e
}

func (e *env) client(t *testing.T, token string) *sdk.APIClient {
	t.Helper()
	opts := []utils.ClientOption{utils.WithToken(token)}
	if e.endpoint != "" {
		opts = append(opts, utils.WithEndpoint(e.endpoint))
	}
	c, err := utils.NewClient(opts...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c.GetAPIClient()
}

// soaSerial は公開されている SOA の内容を返す。反映が行われたかの判定に用いる。
func (e *env) soaSerial(t *testing.T) string {
	t.Helper()
	list, _, err := e.api.RecordsAPI.GetRecordCurrents(context.Background(), e.zoneID).
		KeywordsRrtype(sdk.RECORDSRRTYPE_SOA).ExecuteAll()
	if err != nil {
		t.Fatalf("GetRecordCurrents: %v", err)
	}
	for _, r := range list.Results {
		if r.Rrtype == sdk.RECORDSRRTYPE_SOA && len(r.Rdata) > 0 && r.Rdata[0].Value != nil {
			return *r.Rdata[0].Value
		}
	}
	t.Fatal("SOA が見つからない")
	return ""
}

type staticTokens struct{ token string }

func (s staticTokens) Provider(_, _, _ string) (func(context.Context) (string, error), error) {
	return func(context.Context) (string, error) { return s.token, nil }, nil
}

// newSolver は検証用の Solver を返す。
func (e *env) newSolver(t *testing.T, mutate func(*solver.Options)) *solver.Solver {
	t.Helper()
	o := solver.DefaultOptions()
	o.LockOwner = "e2e-" + time.Now().UTC().Format("150405")
	o.PodNamespace = "e2e"
	if mutate != nil {
		mutate(&o)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if testing.Verbose() {
		log = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	s := solver.NewWithDependencies(o, log, staticTokens{e.token},
		dpf.NewSharedTransport(http.DefaultTransport, dpf.DefaultRateLimit, dpf.DefaultBurst, dpf.DefaultMaxConcurrency))
	return s
}

// config は検証用ゾーンだけを許可する solver 設定である。
func (e *env) config() string {
	ep := e.endpoint
	if ep == "" {
		ep = solver.DefaultEndpoint
	}
	return `{"endpoint":"` + ep + `","domainFilters":["` + e.zone.String() + `"]}`
}
