// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"reflect"
	"sort"
	"testing"
)

// secretReaders は Secret を get できる Role の namespace と、ClusterRole の名前を返す。
// RoleBinding が本サービスの ServiceAccount に結びつけていない Role は数えない。
func secretReaders(t *testing.T, docs []map[string]any) (roleNamespaces, clusterRoles []string) {
	t.Helper()
	grantsSecrets := func(r map[string]any) bool {
		rules, _ := r["rules"].([]any)
		for _, rule := range rules {
			resources, _ := get(rule, "resources").([]any)
			for _, res := range resources {
				if res == "secrets" {
					return true
				}
			}
		}
		return false
	}
	sas := byKind(docs, "ServiceAccount")
	if len(sas) != 1 {
		t.Fatalf("ServiceAccounts = %d", len(sas))
	}
	sa := get(sas[0], "metadata", "name")
	bound := map[string]bool{} // "<namespace>/<role>"
	for _, b := range byKind(docs, "RoleBinding") {
		subjects, _ := b["subjects"].([]any)
		for _, s := range subjects {
			if get(s, "kind") == "ServiceAccount" && get(s, "name") == sa &&
				get(s, "namespace") == "cert-manager" && get(b, "roleRef", "kind") == "Role" {
				bound[get(b, "metadata", "namespace").(string)+"/"+get(b, "roleRef", "name").(string)] = true
			}
		}
	}
	for _, r := range byKind(docs, "Role") {
		ns, _ := get(r, "metadata", "namespace").(string)
		name, _ := get(r, "metadata", "name").(string)
		if grantsSecrets(r) {
			if !bound[ns+"/"+name] {
				t.Errorf("Role %s/%s grants secrets but is not bound to the webhook", ns, name)
			}
			roleNamespaces = append(roleNamespaces, ns)
		}
	}
	for _, r := range byKind(docs, "ClusterRole") {
		if grantsSecrets(r) {
			clusterRoles = append(clusterRoles, get(r, "metadata", "name").(string))
		}
	}
	sort.Strings(roleNamespaces)
	return roleNamespaces, clusterRoles
}

// D-08: 対象 namespace を列挙して Role/RoleBinding のみで構成できる。ClusterIssuer を使わない
// 構成では、ClusterIssuer 用の既定の namespace (本 Pod の namespace) に権限を付与しない。
func TestChartRBACNamespaced(t *testing.T) {
	cases := []struct {
		name string
		sets []string
		want []string
	}{
		{"既定は本 Pod の namespace のみ", nil, []string{"cert-manager"}},
		{"namespace を列挙", []string{"tokenSecrets.namespaces={team-a,team-b}"},
			[]string{"cert-manager", "team-a", "team-b"}},
		{"ClusterIssuer を使わない", []string{"tokenSecrets.namespaces={team-a,team-b}", "tokenSecrets.releaseNamespace=false"},
			[]string{"team-a", "team-b"}},
		{"重複を 1 つにまとめる", []string{"tokenSecrets.namespaces={cert-manager,team-a,team-a}"},
			[]string{"cert-manager", "team-a"}},
		{"いずれも無し", []string{"tokenSecrets.releaseNamespace=false"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			docs, err := renderChart(t, append([]string{"lockOwner=cluster-a"}, c.sets...)...)
			if err != nil {
				t.Fatal(err)
			}
			roles, clusterRoles := secretReaders(t, docs)
			if !reflect.DeepEqual(roles, c.want) {
				t.Errorf("secret-reader Roles in %v, want %v", roles, c.want)
			}
			if len(clusterRoles) != 0 {
				t.Errorf("ClusterRoles grant secrets: %v", clusterRoles)
			}
		})
	}
}
