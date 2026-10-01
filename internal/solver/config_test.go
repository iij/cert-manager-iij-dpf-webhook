// SPDX-License-Identifier: Apache-2.0

package solver

import (
	"errors"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dnsname"
)

func raw(s string) *apiextensionsv1.JSON { return &apiextensionsv1.JSON{Raw: []byte(s)} }

// FR-003: 既定値。FR-019: domainFilters の既定は空 (何も許可しない)。
func TestParseConfigDefaults(t *testing.T) {
	for _, in := range []*apiextensionsv1.JSON{nil, raw(`{}`), raw(`null`)} {
		c, err := parseConfig(in)
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if c.Endpoint != DefaultEndpoint || c.SecretName != "dpf-token" || c.SecretKey != "token" {
			t.Errorf("defaults = %+v", c)
		}
		if !c.Filter.Empty() {
			t.Errorf("domainFilters default must be empty")
		}
	}
}

func TestParseConfigValues(t *testing.T) {
	c, err := parseConfig(raw(`{"endpoint":"https://dpf.example/v1",
		"tokenSecretRef":{"name":"s","key":"k","namespace":"ns"},
		"domainFilters":["example.jp","*.example.jp"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Endpoint != "https://dpf.example/v1" || c.SecretName != "s" || c.SecretKey != "k" || c.SecretNamespace != "ns" {
		t.Errorf("config = %+v", c)
	}
	if !c.Filter.Allows(dnsname.MustParse("a.example.jp")) {
		t.Errorf("filter not applied")
	}
}

// 空文字は既定として扱う (data-model.md)。
func TestParseConfigEmptyStringsUseDefaults(t *testing.T) {
	c, err := parseConfig(raw(`{"tokenSecretRef":{"name":"","key":""}}`))
	if err != nil || c.SecretName != "dpf-token" || c.SecretKey != "token" {
		t.Fatalf("config = %+v, %v", c, err)
	}
}

// FR-004 / FR-005: 未知のフィールドと解釈できない設定は失敗させ、既定値へフォールバックしない。
func TestParseConfigRejects(t *testing.T) {
	for _, in := range []string{
		`{"unknown":1}`,
		`{"tokenSecretRef":{"name":"s","extra":true}}`,
		`{"endpoint":"http://dpf.example/v1"}`,
		`{"endpoint":"dpf.example/v1"}`,
		`{"endpoint":"https://"}`,
		`{"domainFilters":["*example.jp"]}`,
		`{"domainFilters":"example.jp"}`,
		`not json`,
	} {
		if _, err := parseConfig(raw(in)); err == nil {
			t.Errorf("parseConfig(%s) succeeded", in)
		} else if !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("parseConfig(%s) = %v, want ErrInvalidConfig", in, err)
		}
	}
}

// FR-023a: `*` を含む場合は警告を返す。
func TestParseConfigWarnsAllZones(t *testing.T) {
	c, err := parseConfig(raw(`{"domainFilters":["*"]}`))
	if err != nil {
		t.Fatal(err)
	}
	w := c.Warnings()
	if len(w) != 1 || !strings.Contains(w[0], "*") {
		t.Fatalf("warnings = %v", w)
	}
	c, _ = parseConfig(raw(`{"domainFilters":["*.example.jp"]}`))
	if len(c.Warnings()) != 0 {
		t.Fatalf("warnings for a subtree pattern: %v", c.Warnings())
	}
}

// FR-006 / FR-007: トークンを読む namespace の決定。
func TestSecretNamespace(t *testing.T) {
	cases := []struct {
		name      string
		cfg       string
		ambient   bool
		resource  string
		want      string
		wantError bool
	}{
		{"ClusterIssuer: 指定あり", `{"tokenSecretRef":{"namespace":"sec"}}`, true, "cert-manager", "sec", false},
		{"ClusterIssuer: 省略時は本 Pod の namespace", `{}`, true, "cert-manager", "pod-ns", false},
		{"Issuer: ResourceNamespace", `{}`, false, "team-a", "team-a", false},
		{"Issuer: 同じ namespace の指定は受け付ける", `{"tokenSecretRef":{"namespace":"team-a"}}`, false, "team-a", "team-a", false},
		{"Issuer: 他の namespace の指定は失敗し、読み替えない", `{"tokenSecretRef":{"namespace":"team-b"}}`, false, "team-a", "", true},
		{"Issuer: ResourceNamespace が空なら失敗", `{}`, false, "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := parseConfig(raw(c.cfg))
			if err != nil {
				t.Fatal(err)
			}
			got, err := cfg.secretNamespace(c.ambient, c.resource, "pod-ns")
			if c.wantError {
				if !errors.Is(err, ErrNamespaceNotAllowed) {
					t.Fatalf("err = %v, want ErrNamespaceNotAllowed", err)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("namespace = %q, %v; want %q", got, err, c.want)
			}
		})
	}
}
