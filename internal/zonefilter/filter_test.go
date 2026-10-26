// SPDX-License-Identifier: Apache-2.0

package zonefilter_test

import (
	"testing"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/zonefilter"
)

func allows(t *testing.T, patterns []string, zone string) bool {
	t.Helper()
	f, err := zonefilter.Parse(patterns)
	if err != nil {
		t.Fatalf("Parse(%q): %v", patterns, err)
	}
	return f.Allows(dnsname.MustParse(zone))
}

// FR-019: 空または未指定は何も許可しない。
func TestEmptyAllowsNothing(t *testing.T) {
	for _, p := range [][]string{nil, {}} {
		f, err := zonefilter.Parse(p)
		if err != nil {
			t.Fatalf("Parse(%v): %v", p, err)
		}
		if !f.Empty() {
			t.Errorf("Parse(%v).Empty() = false", p)
		}
		if f.Allows(dnsname.MustParse("example.jp")) {
			t.Errorf("empty filter allowed a zone")
		}
	}
}

// FR-020: 3 形式の意味。
func TestPatterns(t *testing.T) {
	cases := []struct {
		patterns []string
		zone     string
		want     bool
	}{
		{[]string{"*"}, "example.jp", true},
		{[]string{"*"}, "a.b.c.test", true},
		{[]string{"*.example.jp"}, "sub.example.jp", true},
		{[]string{"*.example.jp"}, "sub.sub.example.jp", true},
		// FR-021: *.example.jp は example.jp 自身に一致しない。
		{[]string{"*.example.jp"}, "example.jp", false},
		{[]string{"*.example.jp"}, "evilexample.jp", false},
		{[]string{"*.example.jp"}, "example.jp.attacker.test", false},
		{[]string{"example.jp"}, "example.jp", true},
		{[]string{"example.jp"}, "sub.example.jp", false},
		{[]string{"example.jp"}, "evilexample.jp", false},
		{[]string{"example.jp", "*.example.jp"}, "example.jp", true},
		{[]string{"example.jp", "*.example.jp"}, "a.example.jp", true},
	}
	for _, c := range cases {
		if got := allows(t, c.patterns, c.zone); got != c.want {
			t.Errorf("%v allows %s = %v, want %v", c.patterns, c.zone, got, c.want)
		}
	}
}

// FR-022 / FR-023: 3 形式以外は拒否し、残りのパターンで続行しない。
func TestRejectsInvalidPatterns(t *testing.T) {
	for _, p := range [][]string{
		{"*example.jp"},
		{"ex*ample.jp"},
		{"example.*.jp"},
		{"example.jp.*"},
		{"**.example.jp"},
		{""},
		{"example.jp", "*example.jp"},
		{"exa mple.jp"},
	} {
		if _, err := zonefilter.Parse(p); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", p)
		}
	}
}

// FR-025: 大文字小文字と末尾ドットで結果が変わらない。
func TestNormalization(t *testing.T) {
	if !allows(t, []string{"Example.JP."}, "example.jp") {
		t.Errorf("case/trailing dot changed the result (exact)")
	}
	if !allows(t, []string{"*.EXAMPLE.jp"}, "Sub.Example.JP.") {
		t.Errorf("case/trailing dot changed the result (subtree)")
	}
}

// FR-023a: `*` を含むかどうかを判別できる。
func TestAllowsAllZones(t *testing.T) {
	f, _ := zonefilter.Parse([]string{"example.jp", "*"})
	if !f.AllowsAllZones() {
		t.Errorf("AllowsAllZones() = false with '*'")
	}
	f, _ = zonefilter.Parse([]string{"*.example.jp"})
	if f.AllowsAllZones() {
		t.Errorf("AllowsAllZones() = true without '*'")
	}
}
