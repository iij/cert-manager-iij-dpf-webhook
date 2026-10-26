// SPDX-License-Identifier: Apache-2.0

package dnsname_test

import (
	"testing"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dnsname"
)

func TestParseCanonicalizes(t *testing.T) {
	cases := map[string]string{
		"Example.JP":                "example.jp.",
		"example.jp.":               "example.jp.",
		"_ACME-Challenge.WWW.ex.jp": "_acme-challenge.www.ex.jp.",
	}
	for in, want := range cases {
		n, err := dnsname.Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", in, err)
		}
		if got := n.String(); got != want {
			t.Errorf("Parse(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	for _, in := range []string{"", "a..b", "*.example.jp", "foo*.example.jp", "exa mple.jp"} {
		if _, err := dnsname.Parse(in); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", in)
		}
	}
}

func TestEqualIgnoresCaseAndTrailingDot(t *testing.T) {
	a := dnsname.MustParse("Example.JP")
	b := dnsname.MustParse("example.jp.")
	if !a.Equal(b) {
		t.Errorf("%v.Equal(%v) = false", a, b)
	}
	if a.Equal(dnsname.MustParse("example.com")) {
		t.Errorf("different names compared equal")
	}
}

func TestIsSubDomainOf(t *testing.T) {
	zone := dnsname.MustParse("example.jp")
	cases := []struct {
		name string
		want bool
	}{
		{"example.jp", true},
		{"a.example.jp", true},
		{"a.b.example.jp", true},
		{"evilexample.jp", false},
		{"example.jp.attacker.test", false},
		{"jp", false},
	}
	for _, c := range cases {
		if got := dnsname.MustParse(c.name).IsSubDomainOf(zone); got != c.want {
			t.Errorf("%s.IsSubDomainOf(%s) = %v, want %v", c.name, zone, got, c.want)
		}
	}
}

func TestZeroValue(t *testing.T) {
	var n dnsname.Name
	if !n.IsZero() {
		t.Errorf("zero value is not IsZero")
	}
	if dnsname.MustParse("example.jp").IsZero() {
		t.Errorf("parsed name is IsZero")
	}
}
