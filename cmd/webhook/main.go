// SPDX-License-Identifier: Apache-2.0

// Command webhook は cert-manager の DNS01 solver を APIService として起動する。
//
// 起動処理のみを置き、テスト対象から外す (plan.md の Structure Decision)。
// 起動時の挙動は CI で検証する。
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/observability"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/solver"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/version"
)

// groupName は solver の API グループ (FR-001)。
const groupName = "acme.dns-platform.jp"

// envLogLevel はログレベルを与える環境変数である。
const envLogLevel = "DPF_WEBHOOK_LOG_LEVEL"

func main() {
	level, levelErr := observability.ParseLevel(os.Getenv(envLogLevel))
	// stdoutLog は標準出力のみへ出す。テレメトリの送出の失敗の記録に用いる (送出先へ戻さない)。
	stdoutLog := observability.NewLogger(os.Stdout, level)
	if levelErr != nil {
		stdoutLog.Error("invalid configuration", "env", envLogLevel, "error", levelErr)
		os.Exit(1)
	}

	// OTLP 送出は送出先が設定された場合のみ有効になる (原則 V / VI)。
	tel, err := observability.Setup(context.Background(), stdoutLog)
	if err != nil {
		stdoutLog.Error("invalid telemetry configuration", "error", err)
		os.Exit(1)
	}
	log := stdoutLog
	if tel.LogHandler != nil {
		log = observability.NewLogger(os.Stdout, level, tel.LogHandler)
	}
	slog.SetDefault(log)
	metrics, err := observability.NewMetrics(tel.MeterProvider)
	if err != nil {
		log.Error("metrics", "error", err)
		os.Exit(1)
	}

	// 必須設定が不足していれば起動を中止する (FR-050)。
	opts, err := solver.OptionsFromEnv(os.Getenv)
	if err != nil {
		log.Error("invalid deployment options", "error", err)
		os.Exit(1)
	}
	log.Info("starting", "version", version.String(), "lockOwner", opts.LockOwner,
		"operationDeadline", opts.Deadline.String(), "otlp", tel.Exports)

	s := solver.New(opts, log, solver.WithTelemetry(tel.TracerProvider, metrics))
	go func() {
		h := observability.NewExposedHandler(s.Ready, tel.Registry)
		if err := observability.ServeExposed(context.Background(), h); err != nil {
			log.Error("exposed server stopped", "error", err)
			os.Exit(1)
		}
	}()

	cmd.RunWebhookServer(groupName, s)
}
