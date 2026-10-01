// SPDX-License-Identifier: Apache-2.0

// Package observability はログ・メトリクス・ヘルスチェックを提供する (憲章 原則 V)。
package observability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
)

// ParseLevel はログレベルを解釈する。空なら info である。
func ParseLevel(s string) (slog.Level, error) {
	if s == "" {
		return slog.LevelInfo, nil
	}
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return 0, fmt.Errorf("invalid log level %q", s)
	}
	return l, nil
}

// NewLogger は構造化ログ (JSON) を stdout へ出力するロガーを返す。
//
// 標準出力への出力は常に有効であり、extra に渡した他の出力先 (OTLP など) が失敗しても
// 止まらない。extra の失敗は捨てる。
func NewLogger(stdout io.Writer, level slog.Level, extra ...slog.Handler) *slog.Logger {
	primary := slog.NewJSONHandler(stdout, &slog.HandlerOptions{Level: level})
	return slog.New(&fanout{primary: primary, extra: extra})
}

type fanout struct {
	primary slog.Handler
	extra   []slog.Handler
}

func (f *fanout) Enabled(ctx context.Context, l slog.Level) bool {
	return f.primary.Enabled(ctx, l)
}

func (f *fanout) Handle(ctx context.Context, r slog.Record) error {
	err := f.primary.Handle(ctx, r)
	for _, h := range f.extra {
		if h.Enabled(ctx, r.Level) {
			_ = h.Handle(ctx, r.Clone()) // 他の出力先の失敗で標準出力を止めない。
		}
	}
	return err
}

func (f *fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	n := &fanout{primary: f.primary.WithAttrs(attrs)}
	for _, h := range f.extra {
		n.extra = append(n.extra, h.WithAttrs(attrs))
	}
	return n
}

func (f *fanout) WithGroup(name string) slog.Handler {
	n := &fanout{primary: f.primary.WithGroup(name)}
	for _, h := range f.extra {
		n.extra = append(n.extra, h.WithGroup(name))
	}
	return n
}
