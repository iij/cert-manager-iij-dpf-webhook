// SPDX-License-Identifier: Apache-2.0

// Package dnsname はドメイン名を正規化名として扱う型を提供する。
//
// 名前は境界で一度だけ正規化し、以降は正規化済みであることを型で保証する
// (憲章「ドメイン名の取り扱い」)。判定と変換は github.com/miekg/dns に委ね、
// strings パッケージで実装しない。
package dnsname

import (
	"errors"
	"fmt"

	"github.com/miekg/dns"
)

// ErrInvalid は名前が DNS の名前として妥当でない場合に返される。
var ErrInvalid = errors.New("dnsname: invalid domain name")

// Name は正規化名 (小文字・末尾ドット付き) である。ゼロ値は「名前が無い」ことを表す。
type Name struct {
	s string
}

// Parse は s を検証して正規化名を返す。ワイルドカードを含む名前は受け付けない。
// ワイルドカードを伴うパターンは zonefilter が明示的に扱う。
func Parse(s string) (Name, error) {
	if s == "" {
		return Name{}, fmt.Errorf("%w: empty", ErrInvalid)
	}
	if _, ok := dns.IsDomainName(s); !ok {
		return Name{}, fmt.Errorf("%w: %q", ErrInvalid, s)
	}
	c := dns.CanonicalName(s)
	for _, label := range dns.SplitDomainName(c) {
		if !validLabel(label) {
			return Name{}, fmt.Errorf("%w: %q", ErrInvalid, s)
		}
	}
	return Name{s: c}, nil
}

// MustParse は Parse と同じだが、失敗した場合は panic する。定数とテストに用いる。
func MustParse(s string) Name {
	n, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return n
}

// validLabel はラベルが英数字・ハイフン・アンダースコアのみからなるかを返す。
// ワイルドカード (*) や空白を含むラベルを拒否する。
func validLabel(label string) bool {
	if label == "" {
		return false
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// String は正規化名を返す。
func (n Name) String() string { return n.s }

// IsZero は名前が無いことを返す。
func (n Name) IsZero() bool { return n.s == "" }

// Equal は 2 つの正規化名が一致するかを返す。
func (n Name) Equal(o Name) bool { return n.s == o.s }

// IsSubDomainOf は n が parent と一致するか、parent の配下にあるかをラベル境界で返す。
func (n Name) IsSubDomainOf(parent Name) bool {
	if n.IsZero() || parent.IsZero() {
		return false
	}
	return dns.IsSubDomain(parent.s, n.s)
}

// Labels は名前のラベルを返す。
func (n Name) Labels() []string { return dns.SplitDomainName(n.s) }
