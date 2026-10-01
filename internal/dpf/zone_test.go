// SPDX-License-Identifier: Apache-2.0

package dpf_test

import (
	"context"
	"errors"
	"testing"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
)

func zoneServer(t *testing.T) *dpftest.Server {
	return dpftest.New(t,
		dpftest.StandardZone("zparent", "example.com."),
		dpftest.StandardZone("zchild", "sub.example.com."),
	)
}

// FR-013: ResolvedZone と一致するゾーンを選ぶ。
func TestFindZoneExact(t *testing.T) {
	c := newClient(t, zoneServer(t))
	z, err := c.FindZone(context.Background(),
		dnsname.MustParse("sub.example.com"), dnsname.MustParse("_acme-challenge.www.sub.example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if z.ID != "zchild" || z.Name.String() != "sub.example.com." {
		t.Fatalf("zone = %+v", z)
	}
}

// FR-013 / D-18: チャレンジ対象の FQDN と longest match するゾーンが ResolvedZone の子ゾーン
// であっても選ばない。
func TestFindZoneIgnoresUndelegatedChild(t *testing.T) {
	c := newClient(t, zoneServer(t))
	z, err := c.FindZone(context.Background(),
		dnsname.MustParse("example.com"), dnsname.MustParse("_acme-challenge.www.sub.example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if z.ID != "zparent" {
		t.Fatalf("zone = %+v, want zparent", z)
	}
}

// FR-013 / FR-015: ResolvedZone が無く親ゾーンだけがある場合は親ゾーンへ読み替えず失敗する。
// utils.GetZoneFromName は親ゾーンを返すため、結果の名前を確かめる必要がある。
func TestFindZoneDoesNotFallBackToParent(t *testing.T) {
	c := newClient(t, zoneServer(t))
	_, err := c.FindZone(context.Background(),
		dnsname.MustParse("other.example.com"), dnsname.MustParse("_acme-challenge.other.example.com"))
	if !errors.Is(err, dpf.ErrZoneNotFound) {
		t.Fatalf("err = %v, want ErrZoneNotFound", err)
	}
	for _, want := range []string{"other.example.com.", "_acme-challenge.other.example.com."} {
		if !contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	if !dpf.IsPermanent(err) {
		t.Errorf("zone not found should be permanent")
	}
}

func TestFindZoneNotFoundAtAll(t *testing.T) {
	c := newClient(t, zoneServer(t))
	_, err := c.FindZone(context.Background(),
		dnsname.MustParse("example.net"), dnsname.MustParse("_acme-challenge.example.net"))
	if !errors.Is(err, dpf.ErrZoneNotFound) {
		t.Fatalf("err = %v", err)
	}
}

// FR-014: ResolvedFQDN が ResolvedZone の配下に無ければ失敗する。
func TestFindZoneRejectsNameOutsideZone(t *testing.T) {
	c := newClient(t, zoneServer(t))
	_, err := c.FindZone(context.Background(),
		dnsname.MustParse("example.com"), dnsname.MustParse("_acme-challenge.evilexample.com"))
	if !errors.Is(err, dpf.ErrNameOutsideZone) {
		t.Fatalf("err = %v, want ErrNameOutsideZone", err)
	}
}

// FR-025: 比較は正規化名で行う (DPF 側の表記が大文字や末尾ドットなしでも一致する)。
func TestFindZoneCanonicalComparison(t *testing.T) {
	srv := dpftest.New(t, dpftest.StandardZone("zx", "Example.ORG"))
	c := newClient(t, srv)
	z, err := c.FindZone(context.Background(),
		dnsname.MustParse("example.org."), dnsname.MustParse("_acme-challenge.example.org"))
	if err != nil || z.ID != "zx" {
		t.Fatalf("zone = %+v, err = %v", z, err)
	}
}
