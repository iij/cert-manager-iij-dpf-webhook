// SPDX-License-Identifier: Apache-2.0

// Package contract は cert-manager の webhook.Solver の契約を固定する (憲章 原則 I)。
package contract

import (
	"io"
	"log/slog"
	"reflect"
	"testing"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook"
	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	sdk "github.com/iij/dpf-go"
	restclient "k8s.io/client-go/rest"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf/dpftest"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/solver"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/testutil"
)

func init() { sdk.SyncWaitPollInterval = 0 }

var _ webhook.Solver = (*solver.Solver)(nil)

// FR-001: solver 名は iij-dpf。
func TestName(t *testing.T) {
	if got := solver.New(solver.DefaultOptions(), slog.New(slog.NewTextHandler(io.Discard, nil))).Name(); got != "iij-dpf" {
		t.Fatalf("Name() = %q", got)
	}
}

// FR-050: Initialize は配備単位の設定を検証し、不足していればエラーを返す。
func TestInitializeValidatesOptions(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &restclient.Config{Host: "https://127.0.0.1:1"}

	bad := solver.DefaultOptions()
	bad.PodNamespace = "cert-manager"
	if err := solver.New(bad, log).Initialize(cfg, nil); err == nil {
		t.Fatal("Initialize accepted options without a lock owner")
	}

	good := bad
	good.LockOwner = "cluster-a"
	s := solver.New(good, log)
	if s.Ready() {
		t.Fatal("ready before Initialize")
	}
	if err := s.Initialize(cfg, nil); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if !s.Ready() {
		t.Fatal("not ready after Initialize")
	}
}

// FR-037: Present と CleanUp は冪等である。
func TestIdempotent(t *testing.T) {
	srv := dpftest.NewTLS(t, dpftest.StandardZone("zone0000000001", "example.jp."))
	s, _ := testutil.NewSolver(t, srv, nil)
	cfg := testutil.Config(srv, []string{"example.jp"}, nil)
	present := testutil.Request(whapi.ChallengeActionPresent, "example.jp.", "_acme-challenge.example.jp.", "KEY", cfg)
	cleanup := testutil.Request(whapi.ChallengeActionCleanUp, "example.jp.", "_acme-challenge.example.jp.", "KEY", cfg)

	state := func() dpftest.Record {
		srv.Lock()
		defer srv.Unlock()
		r := srv.Zone("zone0000000001").Find("_acme-challenge.example.jp.", "TXT")
		if r == nil {
			t.Fatal("challenge record missing")
		}
		c := *r
		return c
	}

	for i := 0; i < 2; i++ {
		if err := s.Present(present); err != nil {
			t.Fatalf("Present #%d: %v", i, err)
		}
	}
	afterPresent := state()
	for i := 0; i < 2; i++ {
		if err := s.CleanUp(cleanup); err != nil {
			t.Fatalf("CleanUp #%d: %v", i, err)
		}
	}
	if got := state(); !reflect.DeepEqual(got, afterPresent) {
		t.Fatalf("CleanUp changed an unexpired record:\n got  %+v\n want %+v", got, afterPresent)
	}
	srv.Lock()
	defer srv.Unlock()
	if srv.Atomics != 1 {
		t.Fatalf("Atomics = %d, want 1", srv.Atomics)
	}
}
