// SPDX-License-Identifier: Apache-2.0

// Package serving は webhook エンドポイントの TLS の待ち受けを行う。
//
// k8s.io/apiserver はクライアント証明書を任意 (tls.RequestClientCert) とし、証明書を提示しない
// 接続をハンドシェイクで拒否しない。本パッケージは集約層向け CA による検証を必須とし、
// 集約層以外から到達した接続をハンドシェイクで拒否する (FR-002 / research.md D-22)。
package serving

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"k8s.io/apiserver/pkg/server/dynamiccertificates"
)

// HTTP/2 の上限は k8s.io/apiserver (pkg/server/secure_serving.go) と揃える。
const (
	// resourceBody99Percentile は 1 フレーム・1 ストリームの受信バッファの上限である。
	resourceBody99Percentile = 256 * 1024
	// maxConcurrentStreams は 1 接続あたりの同時ストリーム数の上限である。
	maxConcurrentStreams = 100
)

// Options は webhook エンドポイントの待ち受けの設定である。
type Options struct {
	// ServingCert はサーバ証明書である。更新に追随する。
	ServingCert dynamiccertificates.CertKeyContentProvider
	// ClientCA は集約層向け CA (requestheader CA) である。更新に追随する。
	ClientCA dynamiccertificates.CAContentProvider
	// MinVersion は TLS の最小版である。0 の場合は TLS 1.2 とする。
	MinVersion uint16
	// CipherSuites は TLS 1.2 の暗号スイートである。空の場合は Go の既定を用いる。
	CipherSuites []uint16
	// ShutdownTimeout は停止時に処理中の要求を待つ上限である。
	ShutdownTimeout time.Duration
}

// Serve は ln で h を TLS で提供する。ctx が終わると停止する。
// クライアント証明書を提示しない接続と、ClientCA で検証できない証明書を提示する接続は
// ハンドシェイクで拒否され、h に至らない。
func Serve(ctx context.Context, ln net.Listener, h http.Handler, o Options, log *slog.Logger) error {
	if o.ServingCert == nil || o.ClientCA == nil {
		return errors.New("serving: ServingCert and ClientCA are required")
	}
	srv := &http.Server{
		Handler:   h,
		TLSConfig: tlsConfig(o),
		HTTP2: &http.HTTP2Config{
			MaxConcurrentStreams:          maxConcurrentStreams,
			MaxReadFrameSize:              resourceBody99Percentile,
			MaxReceiveBufferPerStream:     resourceBody99Percentile,
			MaxReceiveBufferPerConnection: resourceBody99Percentile * maxConcurrentStreams,
		},
		MaxHeaderBytes:    1 << 20,
		IdleTimeout:       90 * time.Second,
		ReadHeaderTimeout: 32 * time.Second,
		// ハンドシェイクの拒否は集約層以外からの到達を表すため、警告として残す。
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), o.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			log.Warn("webhook server shutdown", "error", err)
		}
	}()
	log.Info("serving webhook", "addr", ln.Addr().String(), "clientCA", o.ClientCA.Name())
	// 証明書は GetConfigForClient が与えるため、ファイルの名前は渡さない。
	err := srv.ServeTLS(ln, "", "")
	if errors.Is(err, http.ErrServerClosed) {
		<-stopped
		return nil
	}
	return err
}

// tlsConfig は接続ごとに現在のサーバ証明書と CA を用いる tls.Config を返す。
func tlsConfig(o Options) *tls.Config {
	minVersion := o.MinVersion
	if minVersion == 0 {
		minVersion = tls.VersionTLS12
	}
	certs := &certCache{provider: o.ServingCert}
	base := &tls.Config{
		MinVersion:   minVersion,
		CipherSuites: o.CipherSuites,
		NextProtos:   []string{"h2", "http/1.1"},
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}
	cfg := base.Clone()
	cfg.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) {
		opts, ok := o.ClientCA.VerifyOptions()
		// ClientCAs が空だとシステムの CA で検証される。CA が得られない間はすべて拒否する。
		if !ok || opts.Roots == nil {
			return nil, fmt.Errorf("serving: client CA %q is not available", o.ClientCA.Name())
		}
		cert, err := certs.current()
		if err != nil {
			return nil, err
		}
		c := base.Clone()
		c.ClientCAs = opts.Roots
		c.Certificates = []tls.Certificate{*cert}
		return c, nil
	}
	return cfg
}

// certCache はサーバ証明書の解析結果を、内容が変わるまで再利用する。
type certCache struct {
	provider dynamiccertificates.CertKeyContentProvider

	mu      sync.Mutex
	certPEM []byte
	keyPEM  []byte
	cert    *tls.Certificate
}

func (c *certCache) current() (*tls.Certificate, error) {
	certPEM, keyPEM := c.provider.CurrentCertKeyContent()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cert != nil && bytes.Equal(certPEM, c.certPEM) && bytes.Equal(keyPEM, c.keyPEM) {
		return c.cert, nil
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("serving: invalid serving certificate %q: %w", c.provider.Name(), err)
	}
	c.certPEM, c.keyPEM, c.cert = certPEM, keyPEM, &cert
	return c.cert, nil
}
