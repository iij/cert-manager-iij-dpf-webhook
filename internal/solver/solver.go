// SPDX-License-Identifier: Apache-2.0

// Package solver は cert-manager の webhook.Solver を実装する。cert-manager の契約に対する
// 薄い変換層であり、DPF 固有の処理は internal/dpf に、判断は internal/challenge に委ねる。
package solver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"github.com/iij/dpf-go/misc/k8s"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/challenge"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/observability"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/tokensource"
)

// SolverName は solver 名である (FR-001)。
const SolverName = "iij-dpf"

// ErrZoneNotAllowed は対象ゾーンが domainFilters に一致しない場合に返される (FR-016 / FR-019)。
var ErrZoneNotAllowed = errors.New("zone is not allowed by domainFilters")

// ErrNotReady は Initialize の前に要求を受けた場合に返される。
var ErrNotReady = errors.New("solver is not initialized")

// Solver は webhook.Solver の実装である。
type Solver struct {
	opts      Options
	log       *slog.Logger
	transport http.RoundTripper
	clients   *dpf.Clients
	ready     atomic.Bool
	tracer    trace.Tracer
	metrics   *observability.Metrics

	// defaultEndpoint は solver 設定の endpoint を省略したときの接続先である (テストで差し替える)。
	defaultEndpoint string
}

// Dependency は Solver に与える追加の依存である。
type Dependency func(*Solver)

// WithTelemetry はトレースとメトリクスの出力先を与える。与えなければ出力しない。
func WithTelemetry(tp trace.TracerProvider, m *observability.Metrics) Dependency {
	return func(s *Solver) {
		s.tracer = tp.Tracer(observability.InstrumentationName)
		s.metrics = m
	}
}

// New は Solver を作る。DPF クライアントは Initialize で Kubernetes の資格情報を得てから作る。
func New(o Options, log *slog.Logger, deps ...Dependency) *Solver {
	return newSolver(o, log,
		dpf.NewSharedTransport(nil, dpf.DefaultRateLimit, dpf.DefaultBurst, dpf.DefaultMaxConcurrency), deps)
}

// NewWithDependencies は依存を与えて Solver を作る。Initialize を経ずに要求を処理できる。
func NewWithDependencies(o Options, log *slog.Logger, tokens dpf.TokenProviders, transport http.RoundTripper,
	deps ...Dependency,
) *Solver {
	s := newSolver(o, log, transport, deps)
	s.clients = dpf.NewClients(transport, tokens)
	s.ready.Store(true)
	return s
}

func newSolver(o Options, log *slog.Logger, transport http.RoundTripper, deps []Dependency) *Solver {
	m, _ := observability.NewMetrics(metricnoop.NewMeterProvider()) // noop は失敗しない
	s := &Solver{
		opts:            o,
		log:             log,
		transport:       transport,
		tracer:          tracenoop.NewTracerProvider().Tracer(observability.InstrumentationName),
		metrics:         m,
		defaultEndpoint: DefaultEndpoint,
	}
	for _, d := range deps {
		d(s)
	}
	return s
}

// Ready は要求を処理できる状態かを返す (readiness probe に用いる)。
func (s *Solver) Ready() bool { return s.ready.Load() }

// Name は solver 名を返す。
func (s *Solver) Name() string { return SolverName }

// Initialize は配備単位の設定を検証し、Kubernetes の資格情報から DPF クライアントを用意する。
// 設定が不足していればエラーを返し、起動を中止させる (FR-050)。
func (s *Solver) Initialize(kubeClientConfig *restclient.Config, _ <-chan struct{}) error {
	if err := s.opts.Validate(); err != nil {
		return err
	}
	cs, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return fmt.Errorf("kubernetes client: %w", err)
	}
	s.clients = dpf.NewClients(s.transport, tokensource.New(k8s.NewSecretsAPI(cs)))
	s.ready.Store(true)
	s.log.Info("initialized", "lockOwner", s.opts.LockOwner, "podNamespace", s.opts.PodNamespace)
	return nil
}

// Present はチャレンジコードを置く (FR-028〜032 / FR-077〜079)。
func (s *Solver) Present(ch *whapi.ChallengeRequest) error {
	return s.handle(ch, challenge.Present)
}

// CleanUp は後片付けである。期限切れの回収のみを行い、当該チャレンジの RDATA と記録を
// 削除しない (FR-082)。
func (s *Solver) CleanUp(ch *whapi.ChallengeRequest) error {
	return s.handle(ch, challenge.CleanUp)
}

// handle は 1 回の要求を処理する (research.md D-11)。
//
//  1. solver 設定を解釈し、トークンを読む namespace を決める
//  2. ResolvedZone と一致するゾーンを特定し、domainFilters を判定する
//  3. 排他を取らずに公開レコードを読み、変更が不要ならここで終える (変更 API は 0 回)
//  4. 排他の下で読み直し、計画を反映する。書き込む直前に domainFilters を再判定する
//
// 要求の受信を span とし、DPF の呼び出しをその子とする (FR-091)。失敗はログに全文を記録し、
// cert-manager へは dpf.Public で記録を除いたものを返す (FR-088b)。
func (s *Solver) handle(ch *whapi.ChallengeRequest, action challenge.Action) (err error) {
	name := actionName(action)
	log := s.log.With("uid", string(ch.UID), "action", name,
		"resourceNamespace", ch.ResourceNamespace, "clusterIssuer", ch.AllowAmbientCredentials,
		"fqdn", ch.ResolvedFQDN, "resolvedZone", ch.ResolvedZone)

	ctx, cancel := operationContext(s.opts)
	defer cancel()
	spanName := observability.SpanPresent
	if action == challenge.CleanUp {
		spanName = observability.SpanCleanUp
	}
	ctx, span := s.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attribute.String("action", name)))
	defer span.End()
	ctx = observability.WithTracer(observability.WithLogger(ctx, log), s.tracer)

	defer func() {
		if err != nil {
			kind := failureKind(err)
			if errors.Is(err, challenge.ErrLabelLimit) {
				// 猶予が長すぎるか、同一の名前に対する検証の失敗が繰り返されている (FR-087)。
				log.Warn("record label limit reached", "name", ch.ResolvedFQDN)
			}
			log.Error("challenge failed", "failure", kind, "error", err)
			span.SetAttributes(attribute.String("failure", kind))
			span.SetStatus(codes.Error, kind)
			err = dpf.Public(err)
		}
		s.metrics.RecordRequest(ctx, name, err)
	}()
	if !s.Ready() || s.clients == nil {
		return ErrNotReady
	}

	cfg, err := parseConfig(ch.Config)
	if err != nil {
		return err
	}
	for _, w := range cfg.Warnings() {
		log.Warn(w)
	}
	if cfg.Filter.Empty() {
		log.Warn("no zone can be modified: domainFilters is empty")
		return fmt.Errorf("%w: domainFilters is empty", ErrZoneNotAllowed)
	}
	ns, err := cfg.secretNamespace(ch.AllowAmbientCredentials, ch.ResourceNamespace, s.opts.PodNamespace)
	if err != nil {
		return err
	}
	fqdn, err := dnsname.Parse(ch.ResolvedFQDN)
	if err != nil {
		return fmt.Errorf("resolved FQDN: %w", err)
	}
	zoneName, err := dnsname.Parse(ch.ResolvedZone)
	if err != nil {
		return fmt.Errorf("resolved zone: %w", err)
	}

	endpoint := cfg.Endpoint
	if endpoint == DefaultEndpoint {
		endpoint = s.defaultEndpoint
	}
	client, err := s.clients.Get(dpf.Source{Endpoint: endpoint, Namespace: ns,
		SecretName: cfg.SecretName, SecretKey: cfg.SecretKey})
	if err != nil {
		return err
	}
	zone, err := client.FindZone(ctx, zoneName, fqdn)
	if err != nil {
		return err
	}
	allowed := func() error {
		if !cfg.Filter.Allows(zone.Name) {
			return fmt.Errorf("%w: %s", ErrZoneNotAllowed, zone.Name)
		}
		return nil
	}
	if err := allowed(); err != nil {
		return err
	}

	req := challenge.Request{Action: action, Key: ch.Key, Grace: s.opts.Grace, Threshold: s.opts.Threshold}

	// 排他を取らない事前判定 (FR-084)。
	cur, err := client.ReadChallenge(ctx, zone, fqdn)
	if err != nil {
		return err
	}
	req.Now = time.Now()
	pre, err := challenge.Plan(cur, req)
	if err != nil {
		return err
	}
	if !pre.Changed {
		log.Info("no change needed", "zone", zone.Name.String())
		return nil
	}

	applied, err := client.Apply(ctx, zone, fqdn,
		dpf.ApplyOptions{Owner: s.opts.LockOwner, LockTTL: s.opts.LockTTL, LockWait: s.opts.LockWait},
		allowed,
		func(cur *challenge.Record) (challenge.Result, error) {
			r := req
			r.Now = time.Now()
			return challenge.Plan(cur, r)
		})
	if applied || err != nil {
		s.metrics.RecordChange(ctx, name, err) // FR-090
	}
	if err != nil {
		return err
	}
	log.Info("challenge record updated", "zone", zone.Name.String(), "applied", applied,
		"description", dpf.Description(s.opts.LockOwner))
	return nil
}

func actionName(a challenge.Action) string {
	if a == challenge.Present {
		return "present"
	}
	return "cleanup"
}

// failureKind は失敗を恒久的／一時的に分類する (FR-010)。
func failureKind(err error) string {
	switch {
	case dpf.IsPermanent(err),
		errors.Is(err, ErrInvalidConfig), errors.Is(err, ErrNamespaceNotAllowed),
		errors.Is(err, ErrZoneNotAllowed), errors.Is(err, challenge.ErrLabelLimit),
		errors.Is(err, dnsname.ErrInvalid):
		return dpf.Permanent.String()
	default:
		return dpf.Temporary.String()
	}
}

// operationContext は操作期限の context を返す。
//
// Present / CleanUp には呼び出しの context が渡されないため、本サービス自身が期限を持つ。
// 要求の context から派生させない (FR-057a)。呼び出し元へタイムアウトが返ったのちも処理を
// 完了まで進め、再試行が望む最終状態を見つけて成功することで収束させる (research.md D-15)。
func operationContext(o Options) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), o.Deadline)
}
