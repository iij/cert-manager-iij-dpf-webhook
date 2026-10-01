// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

const chartPath = "../../deploy/chart"

// renderChart は helm template の出力をリソースの一覧にする。helm が無い場合、CI では失敗し、
// 手元ではスキップする。
func renderChart(t *testing.T, sets ...string) ([]map[string]any, error) {
	t.Helper()
	helm, err := exec.LookPath("helm")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("helm が見つからない")
		}
		t.Skip("helm が見つからない")
	}
	args := []string{"template", "test", chartPath, "--namespace", "cert-manager"}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	var stderr bytes.Buffer
	cmd := exec.Command(helm, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, &renderError{msg: stderr.String()}
	}
	var docs []map[string]any
	for _, d := range strings.Split(string(out), "\n---") {
		var m map[string]any
		if err := yaml.Unmarshal([]byte(d), &m); err != nil {
			t.Fatalf("yaml: %v\n%s", err, d)
		}
		if len(m) > 0 {
			docs = append(docs, m)
		}
	}
	return docs, nil
}

type renderError struct{ msg string }

func (e *renderError) Error() string { return e.msg }

func get(m any, path ...string) any {
	for _, p := range path {
		mm, ok := m.(map[string]any)
		if !ok {
			return nil
		}
		m = mm[p]
	}
	return m
}

func byKind(docs []map[string]any, kind string) []map[string]any {
	var out []map[string]any
	for _, d := range docs {
		if d["kind"] == kind {
			out = append(out, d)
		}
	}
	return out
}

// 既定は拒否側: ロックオーナーを与えなければ描画できない (FR-050)。
func TestChartRequiresLockOwner(t *testing.T) {
	if _, err := renderChart(t); err == nil || !strings.Contains(err.Error(), "lockOwner") {
		t.Fatalf("rendering without lockOwner: %v", err)
	}
}

func TestChartUS1(t *testing.T) {
	docs, err := renderChart(t, "lockOwner=cluster-a")
	if err != nil {
		t.Fatal(err)
	}

	// APIService (acme.dns-platform.jp/v1alpha1、Service のポート 443)。
	apis := byKind(docs, "APIService")
	if len(apis) != 1 || get(apis[0], "spec", "group") != "acme.dns-platform.jp" ||
		get(apis[0], "spec", "version") != "v1alpha1" {
		t.Fatalf("APIService = %v", apis)
	}
	if p := get(apis[0], "spec", "service", "port"); p != float64(443) {
		t.Errorf("APIService service port = %v", p)
	}

	// Deployment のセキュリティ設定 (憲章「バイナリの既定拒否」)。
	deps := byKind(docs, "Deployment")
	if len(deps) != 1 {
		t.Fatalf("Deployments = %d", len(deps))
	}
	pod := get(deps[0], "spec", "template", "spec")
	if get(pod, "securityContext", "runAsNonRoot") != true {
		t.Errorf("runAsNonRoot is not true")
	}
	if _, ok := get(pod, "securityContext", "runAsUser").(float64); !ok {
		t.Errorf("runAsUser is not a fixed number")
	}
	if get(pod, "securityContext", "seccompProfile", "type") != "RuntimeDefault" {
		t.Errorf("seccompProfile is not RuntimeDefault")
	}
	containers, _ := get(pod, "containers").([]any)
	if len(containers) == 0 {
		t.Fatal("no containers")
	}
	for _, c := range containers {
		sc := get(c, "securityContext")
		if get(sc, "allowPrivilegeEscalation") != false || get(sc, "readOnlyRootFilesystem") != true {
			t.Errorf("container securityContext = %v", sc)
		}
		drop, _ := get(sc, "capabilities", "drop").([]any)
		if len(drop) != 1 || drop[0] != "ALL" {
			t.Errorf("capabilities.drop = %v", drop)
		}
	}

	// RBAC: Secret は get のみ、ワイルドカードを含まない (D-08)。
	for _, kind := range []string{"Role", "ClusterRole"} {
		for _, r := range byKind(docs, kind) {
			rules, _ := r["rules"].([]any)
			for _, rule := range rules {
				for _, key := range []string{"apiGroups", "resources", "verbs", "resourceNames"} {
					vals, _ := get(rule, key).([]any)
					for _, v := range vals {
						if v == "*" {
							t.Errorf("%s %v has a wildcard in %s", kind, get(r, "metadata", "name"), key)
						}
					}
				}
				resources, _ := get(rule, "resources").([]any)
				for _, res := range resources {
					if res == "secrets" {
						verbs, _ := get(rule, "verbs").([]any)
						if len(verbs) != 1 || verbs[0] != "get" {
							t.Errorf("secrets verbs = %v, want [get]", verbs)
						}
					}
				}
			}
		}
	}

	// トークンの Secret をチャートが作らない。
	if s := byKind(docs, "Secret"); len(s) != 0 {
		t.Errorf("chart creates Secrets: %v", s)
	}
}
