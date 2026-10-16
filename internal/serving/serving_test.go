// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/apiserver/pkg/server/dynamiccertificates"
)

// testCA はテスト用の CA である。
type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

var serial atomic.Int64

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return key
}

func newCA(t *testing.T, name string) *testCA {
	t.Helper()
	key := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(serial.Add(1)),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	return &testCA{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// issue は ca で署名した証明書を、PEM の証明書と鍵の組で返す。
func (ca *testCA) issue(t *testing.T, name string, usage x509.ExtKeyUsage) (certPEM, keyPEM []byte) {
	t.Helper()
	key := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial.Add(1)),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func (ca *testCA) clientCert(t *testing.T, usage x509.ExtKeyUsage) tls.Certificate {
	t.Helper()
	c, k := ca.issue(t, "front-proxy-client", usage)
	pair, err := tls.X509KeyPair(c, k)
	if err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}
	return pair
}

// caProvider は内容を差し替えられる CAContentProvider である (ConfigMap の更新を模す)。
type caProvider struct {
	mu     sync.Mutex
	bundle []byte
}

func (p *caProvider) set(bundle []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.bundle = bundle
}

func (p *caProvider) Name() string                             { return "test-requestheader" }
func (p *caProvider) AddListener(dynamiccertificates.Listener) {}
func (p *caProvider) CurrentCABundleContent() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.bundle
}

func (p *caProvider) VerifyOptions() (x509.VerifyOptions, bool) {
	bundle := p.CurrentCABundleContent()
	if len(bundle) == 0 {
		return x509.VerifyOptions{}, false
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(bundle)
	return x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}, true
}

// syncBuffer は複数の goroutine から書き込まれるログを受ける。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type fixture struct {
	addr     string
	served   atomic.Int64
	serverCA *x509.CertPool
	logs     *syncBuffer
	peers    chan int
}

// start は clientCA を集約層向け CA として webhook エンドポイントを起動する。
func start(t *testing.T, clientCA dynamiccertificates.CAContentProvider) *fixture {
	t.Helper()
	serverCA := newCA(t, "serving-ca")
	certPEM, keyPEM := serverCA.issue(t, "webhook", x509.ExtKeyUsageServerAuth)
	servingCert, err := dynamiccertificates.NewStaticCertKeyContent("serving", certPEM, keyPEM)
	if err != nil {
		t.Fatalf("NewStaticCertKeyContent: %v", err)
	}
	f := &fixture{serverCA: x509.NewCertPool(), logs: &syncBuffer{}, peers: make(chan int, 16)}
	f.serverCA.AddCert(serverCA.cert)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	f.addr = ln.Addr().String()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.served.Add(1)
		f.peers <- len(r.TLS.PeerCertificates)
		_, _ = io.WriteString(w, r.Proto)
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, ln, h, Options{
			ServingCert:     servingCert,
			ClientCA:        clientCA,
			ShutdownTimeout: time.Second,
		}, slog.New(slog.NewTextHandler(f.logs, nil)))
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Serve: %v", err)
		}
	})
	return f
}

// get は cert を提示して GET を送り、応答の本文 (プロトコル名) を返す。
func (f *fixture) get(t *testing.T, version uint16, certs ...tls.Certificate) (string, error) {
	t.Helper()
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:      f.serverCA,
			Certificates: certs,
			MinVersion:   version,
			MaxVersion:   version,
		},
		ForceAttemptHTTP2: true,
	}
	defer tr.CloseIdleConnections()
	c := &http.Client{Transport: tr, Timeout: 5 * time.Second}
	resp, err := c.Get("https://" + f.addr + "/apis/acme.dns-platform.jp/v1alpha1")
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

// assertRejected は要求がハンドラに至らず、TLS ハンドシェイクで拒否されたことを確かめる。
func (f *fixture) assertRejected(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("要求が受理された")
	}
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		t.Fatalf("タイムアウトであり、ハンドシェイクでの拒否ではない: %v", err)
	}
	if n := f.served.Load(); n != 0 {
		t.Fatalf("ハンドラが %d 回呼ばれた", n)
	}
	// サーバ側の記録が書かれるまで待つ。
	deadline := time.Now().Add(2 * time.Second)
	for !bytes.Contains([]byte(f.logs.String()), []byte("TLS handshake error")) {
		if time.Now().After(deadline) {
			t.Fatalf("ハンドシェイクの失敗が記録されていない: %q", f.logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

var versions = []struct {
	name    string
	version uint16
}{
	{"TLS1.2", tls.VersionTLS12},
	{"TLS1.3", tls.VersionTLS13},
}

// FR-002 / SC-010: クライアント証明書を提示しない接続はハンドシェイクで拒否される。
func TestRejectsConnectionWithoutClientCertificate(t *testing.T) {
	for _, v := range versions {
		t.Run(v.name, func(t *testing.T) {
			ca := newCA(t, "requestheader-ca")
			f := start(t, &caProvider{bundle: ca.pem})
			_, err := f.get(t, v.version)
			f.assertRejected(t, err)
		})
	}
}

// FR-002 / SC-010: 集約層向け CA 以外で署名された証明書はハンドシェイクで拒否される。
func TestRejectsCertificateFromOtherCA(t *testing.T) {
	for _, v := range versions {
		t.Run(v.name, func(t *testing.T) {
			ca := newCA(t, "requestheader-ca")
			other := newCA(t, "other-ca")
			f := start(t, &caProvider{bundle: ca.pem})
			_, err := f.get(t, v.version, other.clientCert(t, x509.ExtKeyUsageClientAuth))
			f.assertRejected(t, err)
		})
	}
}

// 集約層向け CA で署名されていても、クライアント認証の用途を持たない証明書は拒否される。
func TestRejectsCertificateWithoutClientAuthUsage(t *testing.T) {
	ca := newCA(t, "requestheader-ca")
	f := start(t, &caProvider{bundle: ca.pem})
	_, err := f.get(t, tls.VersionTLS13, ca.clientCert(t, x509.ExtKeyUsageServerAuth))
	f.assertRejected(t, err)
}

// 集約層向け CA で署名された証明書を提示した接続は、要求に至る。HTTP/2 を用いる。
func TestAcceptsAggregatorCertificate(t *testing.T) {
	for _, v := range versions {
		t.Run(v.name, func(t *testing.T) {
			ca := newCA(t, "requestheader-ca")
			f := start(t, &caProvider{bundle: ca.pem})
			proto, err := f.get(t, v.version, ca.clientCert(t, x509.ExtKeyUsageClientAuth))
			if err != nil {
				t.Fatalf("要求が拒否された: %v", err)
			}
			if proto != "HTTP/2.0" {
				t.Errorf("proto = %q, want HTTP/2.0", proto)
			}
			// 委譲認証 (requestheader) が証明書を検証できるよう、ハンドラに証明書が渡る。
			if n := <-f.peers; n == 0 {
				t.Error("ハンドラにクライアント証明書が渡っていない")
			}
		})
	}
}

// CA が得られない間は、すべての接続を拒否する (システムの CA で検証しない)。
func TestRejectsAllWhileCAUnavailable(t *testing.T) {
	ca := newCA(t, "requestheader-ca")
	f := start(t, &caProvider{})
	_, err := f.get(t, tls.VersionTLS13, ca.clientCert(t, x509.ExtKeyUsageClientAuth))
	f.assertRejected(t, err)
}

// CA の更新に追随する。古い CA の証明書は拒否し、新しい CA の証明書を受け付ける。
func TestFollowsCARotation(t *testing.T) {
	oldCA := newCA(t, "requestheader-ca-old")
	rotated := newCA(t, "requestheader-ca-new")
	p := &caProvider{bundle: oldCA.pem}
	f := start(t, p)
	if _, err := f.get(t, tls.VersionTLS13, oldCA.clientCert(t, x509.ExtKeyUsageClientAuth)); err != nil {
		t.Fatalf("更新前: %v", err)
	}
	<-f.peers
	p.set(rotated.pem)
	if _, err := f.get(t, tls.VersionTLS13, rotated.clientCert(t, x509.ExtKeyUsageClientAuth)); err != nil {
		t.Fatalf("更新後の CA: %v", err)
	}
	<-f.peers
	f.served.Store(0)
	_, err := f.get(t, tls.VersionTLS13, oldCA.clientCert(t, x509.ExtKeyUsageClientAuth))
	f.assertRejected(t, err)
}
