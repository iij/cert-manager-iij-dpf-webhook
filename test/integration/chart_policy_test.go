// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"reflect"
	"strings"
	"testing"
)

const webhookPort = float64(10443) // values.yaml の securePort の既定

func ingressRules(t *testing.T, policy map[string]any) []any {
	t.Helper()
	rules, _ := get(policy, "spec", "ingress").([]any)
	return rules
}

func rulePorts(rule any, key string) []float64 {
	var out []float64
	ports, _ := get(rule, key).([]any)
	for _, p := range ports {
		if n, ok := get(p, "port").(float64); ok {
			out = append(out, n)
		}
	}
	return out
}

// 憲章 v3.3.0 / plan.md: 既定は k8s 標準の NetworkPolicy。webhook ポートへの ingress は
// kube-apiserver の CIDR に限り (未設定なら描画を失敗させる)、egress は既定で作らない。
func TestChartNetworkPolicyKubernetes(t *testing.T) {
	if _, err := renderChart(t, "lockOwner=cluster-a"); err == nil ||
		!strings.Contains(err.Error(), "kubeAPIServerCIDRs") {
		t.Fatalf("rendering without kubeAPIServerCIDRs: %v", err)
	}

	docs, err := renderChart(t, "lockOwner=cluster-a", apiServerCIDR)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(byKind(docs, "CiliumNetworkPolicy")); n != 0 {
		t.Errorf("CiliumNetworkPolicy by default: %d", n)
	}
	nps := byKind(docs, "NetworkPolicy")
	if len(nps) != 1 {
		t.Fatalf("NetworkPolicies = %d", len(nps))
	}
	np := nps[0]
	if got := get(np, "spec", "podSelector", "matchLabels", "app.kubernetes.io/name"); got != "cert-manager-iij-dpf-webhook" {
		t.Errorf("podSelector = %v", get(np, "spec", "podSelector"))
	}
	if got := get(np, "spec", "policyTypes"); !reflect.DeepEqual(got, []any{"Ingress"}) {
		t.Errorf("policyTypes = %v, want [Ingress]", got)
	}
	if e := get(np, "spec", "egress"); e != nil {
		t.Errorf("egress by default: %v", e)
	}
	rules := ingressRules(t, np)
	if len(rules) != 1 {
		t.Fatalf("ingress rules = %v, want only the webhook rule (exposedFrom is empty)", rules)
	}
	if p := rulePorts(rules[0], "ports"); !reflect.DeepEqual(p, []float64{webhookPort}) {
		t.Errorf("webhook rule ports = %v", p)
	}
	from, _ := get(rules[0], "from").([]any)
	if len(from) != 1 || get(from[0], "ipBlock", "cidr") != "10.0.0.1/32" {
		t.Errorf("webhook rule from = %v", from)
	}
	for _, r := range rules {
		if f, _ := get(r, "from").([]any); len(f) == 0 {
			t.Errorf("an ingress rule without sources opens the port to everyone: %v", r)
		}
		peers, _ := get(r, "from").([]any)
		for _, peer := range peers {
			if get(peer, "namespaceSelector") != nil {
				t.Errorf("an ingress source is a namespace (cert-manager must not be a source): %v", peer)
			}
		}
	}
}

// exposed ポートは exposedFrom に限る。egress を与えれば、その規則のみが egress になる。
func TestChartNetworkPolicyOptions(t *testing.T) {
	docs, err := renderChart(t, "lockOwner=cluster-a", apiServerCIDR,
		`json:networkPolicy.exposedFrom=[{"namespaceSelector":{"matchLabels":{"kubernetes.io/metadata.name":"monitoring"}}}]`,
		`json:networkPolicy.egress=[{"to":[{"ipBlock":{"cidr":"192.0.2.10/32"}}],"ports":[{"port":3128,"protocol":"TCP"}]}]`)
	if err != nil {
		t.Fatal(err)
	}
	nps := byKind(docs, "NetworkPolicy")
	if len(nps) != 1 {
		t.Fatalf("NetworkPolicies = %d", len(nps))
	}
	np := nps[0]
	var exposed any
	for _, r := range ingressRules(t, np) {
		if reflect.DeepEqual(rulePorts(r, "ports"), []float64{8080}) {
			exposed = r
		}
	}
	from, _ := get(exposed, "from").([]any)
	if len(from) != 1 || get(from[0], "namespaceSelector", "matchLabels", "kubernetes.io/metadata.name") != "monitoring" {
		t.Errorf("exposed rule = %v", exposed)
	}
	if got := get(np, "spec", "policyTypes"); !reflect.DeepEqual(got, []any{"Ingress", "Egress"}) {
		t.Errorf("policyTypes = %v", got)
	}
	egress, _ := get(np, "spec", "egress").([]any)
	var to []any
	if len(egress) == 1 {
		to, _ = get(egress[0], "to").([]any)
	}
	if len(egress) != 1 || len(to) != 1 || get(to[0], "ipBlock", "cidr") != "192.0.2.10/32" {
		t.Errorf("egress = %v", egress)
	}
}

// Cilium を選べば CiliumNetworkPolicy になり、kube-apiserver をエンティティで表す (CIDR は不要)。
func TestChartNetworkPolicyCilium(t *testing.T) {
	docs, err := renderChart(t, "lockOwner=cluster-a", "networkPolicy.type=cilium",
		`json:networkPolicy.egress=[{"toFQDNs":[{"matchName":"api.dns-platform.jp"}],"toPorts":[{"ports":[{"port":"443","protocol":"TCP"}]}]}]`)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(byKind(docs, "NetworkPolicy")); n != 0 {
		t.Errorf("NetworkPolicy with type=cilium: %d", n)
	}
	cnps := byKind(docs, "CiliumNetworkPolicy")
	if len(cnps) != 1 || cnps[0]["apiVersion"] != "cilium.io/v2" {
		t.Fatalf("CiliumNetworkPolicies = %v", cnps)
	}
	cnp := cnps[0]
	if get(cnp, "spec", "endpointSelector", "matchLabels", "app.kubernetes.io/name") != "cert-manager-iij-dpf-webhook" {
		t.Errorf("endpointSelector = %v", get(cnp, "spec", "endpointSelector"))
	}
	entities := map[string]float64{}
	for _, r := range ingressRules(t, cnp) {
		ents, _ := get(r, "fromEntities").([]any)
		toPorts, _ := get(r, "toPorts").([]any)
		if len(ents) == 0 || len(toPorts) == 0 {
			t.Errorf("ingress rule without entities or ports: %v", r)
			continue
		}
		ports, _ := get(toPorts[0], "ports").([]any)
		for _, e := range ents {
			entities[e.(string)] = asPort(get(ports[0], "port"))
		}
	}
	if entities["kube-apiserver"] != webhookPort || entities["host"] != 8080 || len(entities) != 2 {
		t.Errorf("ingress entities = %v", entities)
	}
	egress, _ := get(cnp, "spec", "egress").([]any)
	var fqdns []any
	if len(egress) == 1 {
		fqdns, _ = get(egress[0], "toFQDNs").([]any)
	}
	if len(fqdns) != 1 || get(fqdns[0], "matchName") != "api.dns-platform.jp" {
		t.Errorf("egress = %v", egress)
	}

	docs, err = renderChart(t, "lockOwner=cluster-a", "networkPolicy.type=cilium")
	if err != nil {
		t.Fatal(err)
	}
	if c := byKind(docs, "CiliumNetworkPolicy"); len(c) != 1 || get(c[0], "spec", "egress") != nil {
		t.Errorf("CiliumNetworkPolicy by default = %v", c)
	}
}

func asPort(v any) float64 {
	switch p := v.(type) {
	case float64:
		return p
	case string:
		var n float64
		for _, c := range p {
			n = n*10 + float64(c-'0')
		}
		return n
	}
	return -1
}

// 未知の種類は描画を失敗させる (既定へ読み替えない)。
func TestChartNetworkPolicyUnknownType(t *testing.T) {
	if _, err := renderChart(t, "lockOwner=cluster-a", apiServerCIDR, "networkPolicy.type=calico"); err == nil {
		t.Fatal("unknown networkPolicy.type was accepted")
	}
}

// HTTP プロキシは与えた場合のみ渡す。Kubernetes API はプロキシを経由させない。
func TestChartProxy(t *testing.T) {
	docs, err := renderChart(t, "lockOwner=cluster-a", apiServerCIDR)
	if err != nil {
		t.Fatal(err)
	}
	for k := range containerEnv(t, docs) {
		if strings.Contains(strings.ToUpper(k), "PROXY") {
			t.Errorf("proxy is configured by default: %s", k)
		}
	}
	docs, err = renderChart(t, "lockOwner=cluster-a", apiServerCIDR,
		"proxy.httpsProxy=http://proxy.example:3128", "proxy.noProxy=.svc\\,.cluster.local")
	if err != nil {
		t.Fatal(err)
	}
	env := containerEnv(t, docs)
	if env["HTTPS_PROXY"] != "http://proxy.example:3128" {
		t.Errorf("HTTPS_PROXY = %q", env["HTTPS_PROXY"])
	}
	if env["NO_PROXY"] != "$(KUBERNETES_SERVICE_HOST),.svc,.cluster.local" {
		t.Errorf("NO_PROXY = %q", env["NO_PROXY"])
	}
}

// 憲章「コンテナイメージとチャートの既定拒否」: requests と limits、restricted、RBAC の範囲。
func TestChartDefaultsRestricted(t *testing.T) {
	docs, err := renderChart(t, "lockOwner=cluster-a", apiServerCIDR)
	if err != nil {
		t.Fatal(err)
	}
	deps := byKind(docs, "Deployment")
	if len(deps) != 1 {
		t.Fatalf("Deployments = %d", len(deps))
	}
	pod := get(deps[0], "spec", "template", "spec")
	for _, k := range []string{"hostNetwork", "hostPID", "hostIPC"} {
		if get(pod, k) == true {
			t.Errorf("%s is enabled", k)
		}
	}
	vols, _ := get(pod, "volumes").([]any)
	for _, v := range vols {
		if get(v, "hostPath") != nil {
			t.Errorf("hostPath volume: %v", v)
		}
	}
	if get(pod, "automountServiceAccountToken") == false {
		t.Errorf("service account token is required for the Secret and delegated authentication")
	}
	if sa, _ := get(pod, "serviceAccountName").(string); sa == "" || sa == "default" {
		t.Errorf("serviceAccountName = %q", sa)
	}
	containers, _ := get(pod, "containers").([]any)
	for _, c := range containers {
		if get(c, "securityContext", "privileged") == true {
			t.Errorf("privileged container")
		}
		if get(c, "securityContext", "runAsNonRoot") == false {
			t.Errorf("runAsNonRoot is false")
		}
		for _, kind := range []string{"requests", "limits"} {
			for _, res := range []string{"cpu", "memory"} {
				if get(c, "resources", kind, res) == nil {
					t.Errorf("resources.%s.%s is not set", kind, res)
				}
			}
		}
	}

	// RBAC は用途 1 (Secret の get) と用途 2 (集約 API サーバ) に閉じる (D-08)。
	allowed := map[string][]string{
		"secrets":    {"get"},
		"configmaps": {"get", "list", "watch"}, // extension-apiserver-authentication に限る
		"iij-dpf":    {"create"},               // cert-manager による solver の呼び出し
	}
	for _, kind := range []string{"Role", "ClusterRole"} {
		for _, r := range byKind(docs, kind) {
			rules, _ := r["rules"].([]any)
			for _, rule := range rules {
				resources, _ := get(rule, "resources").([]any)
				verbs, _ := get(rule, "verbs").([]any)
				for _, res := range resources {
					want, ok := allowed[res.(string)]
					if !ok {
						t.Errorf("%s grants an unexpected resource %v", kind, res)
						continue
					}
					for _, v := range verbs {
						if !contains(want, v.(string)) {
							t.Errorf("%s grants %v on %v", kind, v, res)
						}
					}
					if res == "configmaps" {
						names, _ := get(rule, "resourceNames").([]any)
						if !reflect.DeepEqual(names, []any{"extension-apiserver-authentication"}) {
							t.Errorf("configmaps are not limited: %v", names)
						}
					}
				}
			}
		}
	}
	for _, b := range byKind(docs, "ClusterRoleBinding") {
		ref, _ := get(b, "roleRef", "name").(string)
		if ref != "system:auth-delegator" && !strings.HasSuffix(ref, ":domain-solver") {
			t.Errorf("unexpected ClusterRoleBinding to %s", ref)
		}
	}
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
