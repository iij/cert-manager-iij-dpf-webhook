// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook"
	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd/server"
	logf "github.com/cert-manager/cert-manager/pkg/logs"
	"github.com/spf13/cobra"
	"k8s.io/apiserver/pkg/server/dynamiccertificates"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/component-base/logs"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/serving"
)

// 集約層向け CA の置き場所。k8s.io/apiserver の委譲認証が読むものと同じである。
const (
	authenticationConfigMapNamespace = "kube-system"
	authenticationConfigMapName      = "extension-apiserver-authentication"
	requestHeaderCAKey               = "requestheader-client-ca-file"
)

// runWebhookServer は cert-manager の cmd.RunWebhookServer に代わり、集約 API サーバを起動する。
//
// API の登録・委譲認証・委譲認可は cert-manager と k8s.io/apiserver に任せる。webhook
// エンドポイントの待ち受けだけを serving が行い、クライアント証明書を必須とする (research.md D-22)。
// フラグは cmd.RunWebhookServer と同じものを受け付ける。
func runWebhookServer(log *slog.Logger, solver webhook.Solver) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		// 2 度目の通知で直ちに終了できるよう、1 度目ののちに既定の扱いへ戻す。
		<-ctx.Done()
		stop()
	}()

	logs.InitLogs()
	defer logs.FlushLogs()
	ctrl.SetLogger(logf.Log)

	o := server.NewWebhookServerOptions(groupName, solver)
	cmd := &cobra.Command{
		Short:         "Launch the IIJ DNS Platform Service ACME solver API server",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Validate(args); err != nil {
				return err
			}
			return run(c.Context(), log, o)
		},
	}
	flags := cmd.Flags()
	logf.AddFlags(o.Logging, flags)
	o.RecommendedOptions.AddFlags(flags)
	return cmd.ExecuteContext(ctx)
}

func run(ctx context.Context, log *slog.Logger, o *server.WebhookServerOptions) error {
	config, err := o.Config()
	if err != nil {
		return err
	}
	cs, err := config.Complete().New()
	if err != nil {
		return err
	}
	apiserver := cs.GenericAPIServer
	info := apiserver.SecureServingInfo
	if info == nil || info.Listener == nil || info.Cert == nil {
		return errors.New("secure serving is not configured")
	}
	// SNICerts にはフレームワークが loopback 用の証明書を加える。loopback のクライアントは
	// トークンで認証し、クライアント証明書を提示しないため、本サービスの待ち受けには用いない。
	// 利用者が与えた SNI の証明書は扱わない。
	if len(o.RecommendedOptions.SecureServing.SNICertKeys) > 0 {
		return errors.New("--tls-sni-cert-key is not supported")
	}

	clientCA, err := requestHeaderCA(ctx, o)
	if err != nil {
		return err
	}

	// フレームワークには接続を受け付けないリスナを渡し、本来のリスナは serving が用いる。
	// フレームワークの Serve は、委譲認証が用いる CA とサーバ証明書の更新を始めるため、
	// 呼ばれる必要がある。
	ln := info.Listener
	info.Listener = newIdleListener(ln.Addr())

	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	served := make(chan error, 1)
	go func() {
		served <- serving.Serve(serveCtx, ln, apiserver.Handler, serving.Options{
			ServingCert:     info.Cert,
			ClientCA:        clientCA,
			MinVersion:      info.MinTLSVersion,
			CipherSuites:    info.CipherSuites,
			ShutdownTimeout: apiserver.ShutdownTimeout,
		}, log)
	}()

	runErr := make(chan error, 1)
	go func() { runErr <- apiserver.PrepareRun().RunWithContext(serveCtx) }()

	select {
	case err := <-served:
		// 待ち受けが先に止まった場合は、フレームワークも止める。
		cancel()
		return errors.Join(err, <-runErr)
	case err := <-runErr:
		cancel()
		return errors.Join(err, <-served)
	}
}

// requestHeaderCA は集約層向け CA を返し、その更新を ctx の間追随させる。
// --requestheader-client-ca-file が与えられればそれを、無ければ ConfigMap を用いる。
// 初回の取得に失敗した場合は起動を中止する (k8s.io/apiserver の委譲認証と同じ扱い)。
func requestHeaderCA(ctx context.Context, o *server.WebhookServerOptions) (dynamiccertificates.CAContentProvider, error) {
	authn := o.RecommendedOptions.Authentication
	if authn == nil {
		return nil, errors.New("delegated authentication is not configured")
	}
	type runner interface {
		dynamiccertificates.CAContentProvider
		RunOnce(context.Context) error
		Run(context.Context, int)
	}
	var p runner
	if file := authn.RequestHeader.ClientCAFile; file != "" {
		f, err := dynamiccertificates.NewDynamicCAContentFromFile("requestheader-client-ca", file)
		if err != nil {
			return nil, fmt.Errorf("requestheader client CA: %w", err)
		}
		p = f
	} else {
		client, err := kubeClient(authn.RemoteKubeConfigFile)
		if err != nil {
			return nil, fmt.Errorf("requestheader client CA: %w", err)
		}
		c, err := dynamiccertificates.NewDynamicCAFromConfigMapController("requestheader-client-ca",
			authenticationConfigMapNamespace, authenticationConfigMapName, requestHeaderCAKey, client)
		if err != nil {
			return nil, fmt.Errorf("requestheader client CA: %w", err)
		}
		p = c
	}
	if err := p.RunOnce(ctx); err != nil {
		return nil, fmt.Errorf("requestheader client CA: %w", err)
	}
	go p.Run(ctx, 1)
	return p, nil
}

// kubeClient は委譲認証と同じ接続先 (--authentication-kubeconfig、無ければクラスタ内) の
// クライアントを返す。
func kubeClient(kubeconfig string) (kubernetes.Interface, error) {
	var cfg *rest.Config
	var err error
	if kubeconfig != "" {
		cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}, &clientcmd.ConfigOverrides{}).ClientConfig()
	} else {
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

// idleListener は接続を受け付けないリスナである。Close されるまで Accept は戻らない。
type idleListener struct {
	addr   net.Addr
	closed chan struct{}
	once   sync.Once
}

func newIdleListener(addr net.Addr) *idleListener {
	return &idleListener{addr: addr, closed: make(chan struct{})}
}

func (l *idleListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}

func (l *idleListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *idleListener) Addr() net.Addr { return l.addr }
