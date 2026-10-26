// SPDX-License-Identifier: Apache-2.0

// Package zonefilter は Issuer / ClusterIssuer の domainFilters を解釈し、
// 変更してよいゾーンかを判定する (FR-016〜025)。
//
// 受け付けるパターンは `*` (すべて)、`*.<ドメイン>` (配下のすべて。ドメイン自身を
// 含まない)、`<ドメイン>` (完全一致) の 3 形式に限る。それ以外は解釈せず拒否する。
package zonefilter

import (
	"errors"
	"fmt"

	"github.com/miekg/dns"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dnsname"
)

// ErrInvalidPattern は受け付けない形式のパターンが指定された場合に返される。
var ErrInvalidPattern = errors.New("zonefilter: invalid domainFilters pattern")

type kind int

const (
	kindAll kind = iota
	kindSubtree
	kindExact
)

type pattern struct {
	kind kind
	base dnsname.Name
}

// Filter は解釈済みの domainFilters である。ゼロ値は何も許可しない。
type Filter struct {
	patterns []pattern
}

// Parse は domainFilters を解釈する。1 つでも受け付けない形式があれば失敗し、
// 残りのパターンで続行しない (FR-023)。
func Parse(patterns []string) (Filter, error) {
	var f Filter
	for _, raw := range patterns {
		p, err := parsePattern(raw)
		if err != nil {
			return Filter{}, err
		}
		f.patterns = append(f.patterns, p)
	}
	return f, nil
}

func parsePattern(raw string) (pattern, error) {
	if raw == "*" {
		return pattern{kind: kindAll}, nil
	}
	if raw == "" {
		return pattern{}, fmt.Errorf("%w: empty", ErrInvalidPattern)
	}
	labels := dns.SplitDomainName(raw)
	if len(labels) >= 2 && labels[0] == "*" {
		base, err := dnsname.Parse(dns.Fqdn(joinLabels(labels[1:])))
		if err != nil {
			return pattern{}, fmt.Errorf("%w: %q", ErrInvalidPattern, raw)
		}
		return pattern{kind: kindSubtree, base: base}, nil
	}
	// `*` を含む他の形はすべて dnsname.Parse が拒否する。
	base, err := dnsname.Parse(raw)
	if err != nil {
		return pattern{}, fmt.Errorf("%w: %q", ErrInvalidPattern, raw)
	}
	return pattern{kind: kindExact, base: base}, nil
}

// joinLabels はラベルを名前へ戻す。ラベルは SplitDomainName の出力であり、
// エスケープを含まない前提で dns.Fqdn に渡す前の連結のみを行う。
func joinLabels(labels []string) string {
	out := ""
	for i, l := range labels {
		if i > 0 {
			out += "."
		}
		out += l
	}
	return out
}

// Empty はパターンが 1 つも無いことを返す。この場合、いかなるゾーンも変更しない (FR-019)。
func (f Filter) Empty() bool { return len(f.patterns) == 0 }

// AllowsAllZones は `*` を含むことを返す。既定の拒否を緩める設定であり、
// 警告の記録に用いる (FR-023a)。
func (f Filter) AllowsAllZones() bool {
	for _, p := range f.patterns {
		if p.kind == kindAll {
			return true
		}
	}
	return false
}

// Allows は zone を変更してよいかを返す。判定はラベル境界で行う (FR-024)。
func (f Filter) Allows(zone dnsname.Name) bool {
	if zone.IsZero() {
		return false
	}
	for _, p := range f.patterns {
		switch p.kind {
		case kindAll:
			return true
		case kindExact:
			if zone.Equal(p.base) {
				return true
			}
		case kindSubtree:
			if zone.IsSubDomainOf(p.base) && !zone.Equal(p.base) {
				return true
			}
		}
	}
	return false
}
