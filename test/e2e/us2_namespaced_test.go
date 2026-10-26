// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package e2e

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/challenge"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/solver"
	"github.com/iij/cert-manager-iij-dpf-webhook/test/e2e/acmeserver"
)

// nsTokens は namespace ごとに異なるトークンを返し、読んだ namespace を記録する。
type nsTokens struct {
	mu     sync.Mutex
	tokens map[string]string
	read   []string
}

func (n *nsTokens) Provider(namespace, _, _ string) (func(context.Context) (string, error), error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.read = append(n.read, namespace)
	tok, ok := n.tokens[namespace]
	if !ok {
		return nil, errors.New("no token for namespace " + namespace)
	}
	return func(context.Context) (string, error) { return tok, nil }, nil
}

func (n *nsTokens) namespaces() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.read)
}

// T087 (DPF 側): namespace 限定の Issuer 由来の要求が、それぞれの namespace のトークンで処理され、
// 他の namespace の Secret を指す要求は Secret を読まずに失敗する (FR-006 / FR-007 / SC-008)。
// DPF_API_TOKEN_2 があれば team-b に別のトークンを与える。
func TestUS2NamespacedTokens(t *testing.T) {
	e := loadEnv(t)
	token2 := e.token2
	if token2 == "" {
		token2 = e.token
	}
	tokens := &nsTokens{tokens: map[string]string{"team-a": e.token, "team-b": token2}}
	grace := 30 * time.Second
	o := solver.DefaultOptions()
	o.LockOwner = "e2e-us2-" + time.Now().UTC().Format("150405")
	o.PodNamespace = "e2e"
	o.Grace, o.Threshold = grace, grace/2
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if testing.Verbose() {
		log = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	s := solver.NewWithDependencies(o, log, tokens,
		dpf.NewSharedTransport(http.DefaultTransport, dpf.DefaultRateLimit, dpf.DefaultBurst, dpf.DefaultMaxConcurrency))

	label := "_acme-challenge.e2e-us2"
	name := label + "." + e.zone.String()
	key := func(ns string) string { return "us2-" + ns + "-" + time.Now().UTC().Format("150405") }
	keys := map[string]string{}
	for _, ns := range []string{"team-a", "team-b"} {
		keys[ns] = key(ns)
		r := e.request(whapi.ChallengeActionPresent, label, keys[ns])
		r.AllowAmbientCredentials = false
		r.ResourceNamespace = ns
		if err := s.Present(r); err != nil {
			t.Fatalf("Present (%s): %v", ns, err)
		}
	}
	got := values(e.challengeTXT(t, name))
	for ns, k := range keys {
		if !slices.Contains(got, challenge.QuoteTXT(k)) {
			t.Errorf("%s の RDATA が無い: %v", ns, got)
		}
	}
	if ns := tokens.namespaces(); !slices.Equal(ns, []string{"team-a", "team-b"}) {
		t.Errorf("read namespaces = %v", ns)
	}

	// team-b の Issuer が team-a の Secret を指す。
	r := e.request(whapi.ChallengeActionPresent, label, key("cross"))
	r.AllowAmbientCredentials = false
	r.ResourceNamespace = "team-b"
	r.Config.Raw = []byte(strings.Replace(string(r.Config.Raw), "{", `{"tokenSecretRef":{"namespace":"team-a"},`, 1))
	if err := s.Present(r); !errors.Is(err, solver.ErrNamespaceNotAllowed) {
		t.Fatalf("cross-namespace Present: err = %v, want ErrNamespaceNotAllowed", err)
	}
	if ns := tokens.namespaces(); len(ns) != 2 {
		t.Errorf("cross-namespace request read a secret: %v", ns)
	}

	// 後始末: 期限を過ぎてから回収する。
	time.Sleep(grace + 5*time.Second)
	for _, ns := range []string{"team-a", "team-b"} {
		r := e.request(whapi.ChallengeActionCleanUp, label, "any")
		r.AllowAmbientCredentials = false
		r.ResourceNamespace = ns
		if err := s.CleanUp(r); err != nil {
			t.Fatalf("CleanUp (%s): %v", ns, err)
		}
	}
	if got := values(e.challengeTXT(t, name)); len(got) != 0 {
		t.Errorf("RDATA remain after grace: %v", got)
	}
}

// T087 (クラスタ): 2 つの namespace に別々のトークンと Issuer を置き、それぞれの Certificate が
// Ready になる。他の namespace の Secret を指す Issuer のチャレンジは失敗する (SC-008)。
//
// 本チャートを tokenSecrets.namespaces に E2E_US2_NAMESPACES (既定 e2e-team-a,e2e-team-b) を
// 与えて導入し、その namespace を作っておくこと。ACME サーバは acmeserver.Resolve の値とする。
func TestUS2NamespacedIssuers(t *testing.T) {
	e := loadEnv(t)
	if os.Getenv("E2E_CLUSTER") == "" {
		t.Skip("E2E_CLUSTER (本チャートと cert-manager を導入したクラスタ) が必要")
	}
	namespaces := []string{"e2e-team-a", "e2e-team-b"}
	if v := os.Getenv("E2E_US2_NAMESPACES"); v != "" {
		namespaces = strings.Split(v, ",")
	}
	if len(namespaces) != 2 {
		t.Fatalf("E2E_US2_NAMESPACES は 2 つの namespace を与える: %v", namespaces)
	}
	server, err := acmeserver.Resolve(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	cs := kubernetes.NewForConfigOrDie(cfg)
	dc := dynamic.NewForConfigOrDie(cfg)
	ctx := context.Background()
	ts := time.Now().UTC().Format("150405")
	token2 := e.token2
	if token2 == "" {
		token2 = e.token
	}
	issuerGVR := schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "issuers"}
	certGVR := schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "certificates"}

	create := func(ns string, gvr schema.GroupVersionResource, obj map[string]any) {
		t.Helper()
		u := &unstructured.Unstructured{Object: obj}
		if _, err := dc.Resource(gvr).Namespace(ns).Create(ctx, u, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = dc.Resource(gvr).Namespace(ns).Delete(ctx, u.GetName(), metav1.DeleteOptions{}) })
	}
	issuer := func(ns, name string, solverConfig map[string]any) {
		t.Helper()
		create(ns, issuerGVR, map[string]any{
			"apiVersion": "cert-manager.io/v1", "kind": "Issuer",
			"metadata": map[string]any{"name": name, "namespace": ns},
			"spec": map[string]any{"acme": map[string]any{
				"server": server, "privateKeySecretRef": map[string]any{"name": name + "-account"},
				"solvers": []any{map[string]any{"dns01": map[string]any{"webhook": map[string]any{
					"groupName": "acme.dns-platform.jp", "solverName": "iij-dpf", "config": solverConfig,
				}}}},
			}},
		})
	}
	certificate := func(ns, name, issuerName string) string {
		t.Helper()
		dnsName := strings.TrimSuffix(name+"."+e.zone.String(), ".")
		create(ns, certGVR, map[string]any{
			"apiVersion": "cert-manager.io/v1", "kind": "Certificate",
			"metadata": map[string]any{"name": name, "namespace": ns},
			"spec": map[string]any{"secretName": name, "dnsNames": []any{dnsName},
				"issuerRef": map[string]any{"name": issuerName, "kind": "Issuer"}},
		})
		return dnsName
	}

	for i, ns := range namespaces {
		tok := e.token
		if i == 1 {
			tok = token2
		}
		sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "dpf-token-" + ts, Namespace: ns},
			StringData: map[string]string{"token": tok}}
		if _, err := cs.CoreV1().Secrets(ns).Create(ctx, sec, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cs.CoreV1().Secrets(ns).Delete(ctx, sec.Name, metav1.DeleteOptions{}) })
		issuer(ns, "e2e-dpf-"+ts, map[string]any{"domainFilters": []any{e.zone.String()},
			"tokenSecretRef": map[string]any{"name": sec.Name}})
		certificate(ns, "e2e-us2-"+string(rune('a'+i))+"-"+ts, "e2e-dpf-"+ts)
	}

	// 2 つ目の namespace に、1 つ目の namespace の Secret を指す Issuer を置く。
	cross := namespaces[1]
	issuer(cross, "e2e-dpf-cross-"+ts, map[string]any{"domainFilters": []any{e.zone.String()},
		"tokenSecretRef": map[string]any{"name": "dpf-token-" + ts, "namespace": namespaces[0]}})
	crossDNS := certificate(cross, "e2e-us2-cross-"+ts, "e2e-dpf-cross-"+ts)

	ready := map[string]bool{}
	crossRejected := false
	challengeGVR := schema.GroupVersionResource{Group: "acme.cert-manager.io", Version: "v1", Resource: "challenges"}
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) && (len(ready) < len(namespaces) || !crossRejected) {
		for i, ns := range namespaces {
			name := "e2e-us2-" + string(rune('a'+i)) + "-" + ts
			got, err := dc.Resource(certGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				continue
			}
			conds, _, _ := unstructured.NestedSlice(got.Object, "status", "conditions")
			for _, c := range conds {
				if m, _ := c.(map[string]any); m["type"] == "Ready" && m["status"] == "True" {
					ready[ns] = true
				}
			}
		}
		list, err := dc.Resource(challengeGVR).Namespace(cross).List(ctx, metav1.ListOptions{})
		if err == nil {
			for _, ch := range list.Items {
				dnsName, _, _ := unstructured.NestedString(ch.Object, "spec", "dnsName")
				reason, _, _ := unstructured.NestedString(ch.Object, "status", "reason")
				if dnsName == crossDNS && strings.Contains(reason, solver.ErrNamespaceNotAllowed.Error()) {
					crossRejected = true
				}
			}
		}
		time.Sleep(10 * time.Second)
	}
	for _, ns := range namespaces {
		if !ready[ns] {
			t.Errorf("%s の証明書が Ready にならない", ns)
		}
	}
	if !crossRejected {
		t.Errorf("他の namespace の Secret を指す Issuer のチャレンジが %q で失敗しない", solver.ErrNamespaceNotAllowed)
	}
}
