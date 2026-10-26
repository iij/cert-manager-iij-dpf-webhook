// SPDX-License-Identifier: Apache-2.0

package observability

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

type failingHandler struct{}

func (failingHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (failingHandler) Handle(context.Context, slog.Record) error { return errors.New("export failed") }
func (h failingHandler) WithAttrs([]slog.Attr) slog.Handler      { return h }
func (h failingHandler) WithGroup(string) slog.Handler           { return h }

// 憲章 原則 V: 標準出力への構造化ログは常に有効で、他の出力先の失敗で止まらない。
func TestStdoutAlwaysWritten(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(&buf, slog.LevelInfo, failingHandler{})
	log.Info("hello", "zone", "example.jp.")
	log.With("k", "v").Info("again")
	out := buf.String()
	if !strings.Contains(out, `"msg":"hello"`) || !strings.Contains(out, `"zone":"example.jp."`) {
		t.Fatalf("stdout = %s", out)
	}
	if !strings.Contains(out, `"msg":"again"`) || !strings.Contains(out, `"k":"v"`) {
		t.Fatalf("stdout = %s", out)
	}
}

// ログレベルを設定で変えられる。
func TestLevel(t *testing.T) {
	var buf bytes.Buffer
	lvl, err := ParseLevel("warn")
	if err != nil {
		t.Fatal(err)
	}
	log := NewLogger(&buf, lvl)
	log.Info("hidden")
	log.Warn("shown")
	if strings.Contains(buf.String(), "hidden") || !strings.Contains(buf.String(), "shown") {
		t.Fatalf("stdout = %s", buf.String())
	}
	if _, err := ParseLevel("loud"); err == nil {
		t.Fatal("ParseLevel accepted an unknown level")
	}
	if l, _ := ParseLevel(""); l != slog.LevelInfo {
		t.Fatalf("default level = %v", l)
	}
}
