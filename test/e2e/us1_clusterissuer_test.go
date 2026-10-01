// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package e2e

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	sdk "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/challenge"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/solver"
	"github.com/iij/cert-manager-iij-dpf-webhook/test/e2e/acmeserver"
)

// challengeTXT は公開されている対象名の TXT を返す。無ければ nil。
func (e *env) challengeTXT(t *testing.T, name string) *sdk.Record {
	t.Helper()
	list, _, err := e.api.RecordsAPI.GetRecordCurrents(context.Background(), e.zoneID).ExecuteAll()
	if err != nil {
		t.Fatalf("GetRecordCurrents: %v", err)
	}
	for i, r := range list.Results {
		if r.Rrtype == sdk.RECORDSRRTYPE_TXT && r.Name == name {
			return &list.Results[i]
		}
	}
	return nil
}

func values(r *sdk.Record) []string {
	if r == nil {
		return nil
	}
	var out []string
	for _, v := range r.Rdata {
		if v.Value != nil {
			out = append(out, *v.Value)
		}
	}
	return out
}

// T075 (DPF 側): 提示 → 反映、後片付けでは残り、期限を過ぎてから回収される (SC-024)。
// 変更履歴の Description から配備を判別できる (SC-012)。更新を 10 回繰り返しても猶予の
// 期間より古い RDATA は残らず (SC-006)、他者の RDATA と TTL は元のまま残る (SC-020)。
func TestUS1ChallengeLifecycle(t *testing.T) {
	e := loadEnv(t)
	ctx := context.Background()
	label := "_acme-challenge.e2e-us1"
	name := label + "." + e.zone.String()

	// 他者の RDATA を TTL 300 で置いておく。
	ap := utils.NewZoneApplier(e.api.RecordsAPI, e.api.ZonesAPI, e.api.JobsAPI, e.zoneID,
		utils.WithLockOptions(utils.WithOwner("e2e-setup")))
	ttl := int32(300)
	if err := ap.Apply(ctx, func(_ context.Context, rs []sdk.OverwriteRecordsInner) ([]sdk.OverwriteRecordsInner, error) {
		out := rs[:0:0]
		for _, r := range rs {
			if r.Name != name {
				out = append(out, r)
			}
		}
		return append(out, sdk.OverwriteRecordsInner{Name: name, Ttl: *sdk.NewNullableInt32(&ttl),
			Rrtype: sdk.RECORDSRRTYPE_TXT, Rdata: []sdk.RecordsRdataInner{{Value: sdk.PtrString(`"foreign"`)}},
			Labels: map[string]string{}}), nil
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	grace := 90 * time.Second
	owner := "e2e-" + time.Now().UTC().Format("150405")
	s := e.newSolver(t, func(o *solver.Options) { o.Grace = grace; o.Threshold = grace / 2; o.LockOwner = owner })

	for i := 0; i < 10; i++ {
		key := "us1-" + time.Now().UTC().Format("150405") + "-" + string(rune('a'+i))
		if err := s.Present(e.request(whapi.ChallengeActionPresent, label, key)); err != nil {
			t.Fatalf("Present #%d: %v", i, err)
		}
		if err := s.CleanUp(e.request(whapi.ChallengeActionCleanUp, label, key)); err != nil {
			t.Fatalf("CleanUp #%d: %v", i, err)
		}
		// 後片付けの時点では RDATA と記録が残る (FR-082)。
		rec := e.challengeTXT(t, name)
		if !slices.Contains(values(rec), challenge.QuoteTXT(key)) ||
			rec.Labels[challenge.LabelKey(challenge.TargetID(key))] == "" {
			t.Fatalf("#%d: CleanUp removed the record: %v", i, values(rec))
		}
	}

	// SC-012: 変更履歴の Description から配備を判別できる。
	hist, _, err := e.api.ZoneHistoriesAPI.GetZoneHistoryList(ctx, e.zoneID).Execute()
	if err != nil {
		t.Fatalf("GetZoneHistoryList: %v", err)
	}
	found := false
	for _, h := range hist.Results {
		if h.Description == dpf.Description(owner) {
			found = true
		}
	}
	if !found {
		t.Errorf("変更履歴に %q が無い", dpf.Description(owner))
	}

	// 期限を過ぎてから操作すると回収される (SC-006 / SC-024)。他者の RDATA と TTL は残る (SC-020)。
	time.Sleep(grace + 5*time.Second)
	if err := s.CleanUp(e.request(whapi.ChallengeActionCleanUp, label, "any")); err != nil {
		t.Fatalf("CleanUp after grace: %v", err)
	}
	rec := e.challengeTXT(t, name)
	if got := values(rec); !slices.Equal(got, []string{`"foreign"`}) {
		t.Errorf("RDATA = %v, want only the foreign value", got)
	}
	if rec != nil && (rec.Ttl.Get() == nil || *rec.Ttl.Get() != ttl) {
		t.Errorf("TTL changed: %v", rec.Ttl.Get())
	}
	if rec != nil && len(rec.Labels) != 0 {
		t.Errorf("labels remain: %v", rec.Labels)
	}
}

// T075 (クラスタ): ClusterIssuer と Certificate を作り、証明書が Ready になる (SC-001)。
// ACME サーバは acmeserver.Resolve の値 (既定はステージング環境) とし、本番環境を用いない。
// 本チャートと cert-manager を導入したクラスタ (KUBECONFIG) が必要。
func TestUS1ClusterIssuer(t *testing.T) {
	e := loadEnv(t)
	if os.Getenv("E2E_CLUSTER") == "" {
		t.Skip("E2E_CLUSTER (本チャートと cert-manager を導入したクラスタ) が必要")
	}
	server, err := acmeserver.Resolve(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	dc := dynamic.NewForConfigOrDie(cfg)
	ctx := context.Background()
	ts := time.Now().UTC().Format("150405")

	issuer := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cert-manager.io/v1", "kind": "ClusterIssuer",
		"metadata": map[string]any{"name": "e2e-dpf-" + ts},
		"spec": map[string]any{"acme": map[string]any{
			"server": server, "privateKeySecretRef": map[string]any{"name": "e2e-dpf-account-" + ts},
			"solvers": []any{map[string]any{"dns01": map[string]any{"webhook": map[string]any{
				"groupName": "acme.dns-platform.jp", "solverName": "iij-dpf",
				"config": map[string]any{"domainFilters": []any{e.zone.String()}},
			}}}},
		}},
	}}
	cig := schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "clusterissuers"}
	if _, err := dc.Resource(cig).Create(ctx, issuer, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dc.Resource(cig).Delete(ctx, issuer.GetName(), metav1.DeleteOptions{}) })

	dnsName := "e2e-" + ts + "." + e.zone.String()
	cert := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cert-manager.io/v1", "kind": "Certificate",
		"metadata": map[string]any{"name": "e2e-dpf-" + ts, "namespace": "default"},
		"spec": map[string]any{"secretName": "e2e-dpf-" + ts, "dnsNames": []any{dnsName[:len(dnsName)-1]},
			"issuerRef": map[string]any{"name": issuer.GetName(), "kind": "ClusterIssuer"}},
	}}
	cg := schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "certificates"}
	if _, err := dc.Resource(cg).Namespace("default").Create(ctx, cert, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dc.Resource(cg).Namespace("default").Delete(ctx, cert.GetName(), metav1.DeleteOptions{}) })

	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		got, err := dc.Resource(cg).Namespace("default").Get(ctx, cert.GetName(), metav1.GetOptions{})
		if err == nil {
			conds, _, _ := unstructured.NestedSlice(got.Object, "status", "conditions")
			for _, c := range conds {
				m, _ := c.(map[string]any)
				if m["type"] == "Ready" && m["status"] == "True" {
					return
				}
			}
		}
		time.Sleep(10 * time.Second)
	}
	t.Fatal("証明書が Ready にならない")
}
