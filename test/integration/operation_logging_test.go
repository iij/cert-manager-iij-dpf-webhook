// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"strconv"
	"strings"
	"testing"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/testutil"
)

func hasLine(out string, substr ...string) bool {
next:
	for _, l := range strings.Split(out, "\n") {
		for _, s := range substr {
			if !strings.Contains(l, s) {
				continue next
			}
		}
		return true
	}
	return false
}

// FR-065 / FR-089: ロックの取得・解放、適用した Description、DNS レコードの変更操作
// (対象ゾーン・レコード名・操作種別・結果) を記録する。
func TestOperationLogging(t *testing.T) {
	srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
	var logs testutil.LogBuffer
	s := testutil.NewSolverWith(t, srv, testutil.FixedToken("token"), logs.Logger(), nil)
	cfg := testutil.Config(srv, []string{"example.jp"}, nil)
	if err := s.Present(testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge.example.jp.", "KEY", cfg)); err != nil {
		t.Fatal(err)
	}
	out := logs.String()
	if !hasLine(out, "zone lock acquired", "zone=example.jp.", "owner=cluster-a") {
		t.Errorf("lock acquisition is not logged")
	}
	if !hasLine(out, "zone lock released", "zone=example.jp.") {
		t.Errorf("lock release is not logged")
	}
	if !hasLine(out, "DNS record change", "zone=example.jp.", "name=_acme-challenge.example.jp.",
		"operation=create", "result=success", `description="by cert-manager(cluster-a)"`) {
		t.Errorf("record change is not logged:\n%s", out)
	}

	// 失敗した変更操作も記録する。
	srv.Lock()
	srv.FailAtomic = true
	srv.Unlock()
	if err := s.Present(testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge.example.jp.", "KEY2", cfg)); err == nil {
		t.Fatal("Present succeeded")
	}
	if !hasLine(logs.String(), "DNS record change", "name=_acme-challenge.example.jp.", "operation=update", "result=failure") {
		t.Errorf("failed record change is not logged")
	}
}

// FR-015 / FR-019 / FR-023a / FR-087: ゾーン不在・domainFilters が空・`*`・ラベル上限を記録する。
func TestDiagnosticLogging(t *testing.T) {
	t.Run("ゾーン不在", func(t *testing.T) {
		srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
		var logs testutil.LogBuffer
		s := testutil.NewSolverWith(t, srv, testutil.FixedToken("token"), logs.Logger(), nil)
		_ = s.Present(testutil.Request(whapi.ChallengeActionPresent, "sub.example.jp.", "_acme-challenge.sub.example.jp.", "KEY",
			testutil.Config(srv, []string{"*"}, nil)))
		if !hasLine(logs.String(), dpf.ErrZoneNotFound.Error(), "resolvedZone=sub.example.jp.",
			"fqdn=_acme-challenge.sub.example.jp.", "failure=permanent") {
			t.Errorf("zone not found is not logged:\n%s", logs.String())
		}
	})
	t.Run("domainFilters が空", func(t *testing.T) {
		srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
		var logs testutil.LogBuffer
		s := testutil.NewSolverWith(t, srv, testutil.FixedToken("token"), logs.Logger(), nil)
		_ = s.Present(testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge.example.jp.", "KEY",
			testutil.Config(srv, nil, nil)))
		if !hasLine(logs.String(), "domainFilters is empty") {
			t.Errorf("empty domainFilters is not logged")
		}
	})
	t.Run("* の警告", func(t *testing.T) {
		srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
		var logs testutil.LogBuffer
		s := testutil.NewSolverWith(t, srv, testutil.FixedToken("token"), logs.Logger(), nil)
		_ = s.Present(testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge.example.jp.", "KEY",
			testutil.Config(srv, []string{"*"}, nil)))
		if !hasLine(logs.String(), "level=WARN", `domainFilters contains \"*\"`) {
			t.Errorf("wildcard warning is not logged:\n%s", logs.String())
		}
	})
	t.Run("ラベル上限", func(t *testing.T) {
		z := dpftest.StandardZone("zone0000000001", "example.jp.")
		ttl := int32(60)
		labels := map[string]string{}
		for i := 0; i < 10; i++ {
			labels["foreign"+strconv.Itoa(i)] = "x"
		}
		z.Records = append(z.Records, &dpftest.Record{Name: "_acme-challenge.example.jp.", TTL: &ttl, Rrtype: "TXT",
			Rdata: []string{`"other"`}, Labels: labels})
		srv := dpftest.NewTLS(t, z)
		var logs testutil.LogBuffer
		s := testutil.NewSolverWith(t, srv, testutil.FixedToken("token"), logs.Logger(), nil)
		_ = s.Present(testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge.example.jp.", "KEY",
			testutil.Config(srv, []string{"example.jp"}, nil)))
		if !hasLine(logs.String(), "label limit", "name=_acme-challenge.example.jp.") {
			t.Errorf("label limit is not logged:\n%s", logs.String())
		}
	})
}
