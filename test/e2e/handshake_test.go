// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package e2e

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// TestWebhookRequiresClientCertificate は、集約層を経由せず webhook エンドポイントへ直接つないだ
// 接続が、TLS ハンドシェイクで拒否され要求に至らないことを確かめる (FR-002 / SC-010 / T120)。
//
// E2E_WEBHOOK_ADDR に webhook エンドポイントのアドレス (Pod の IP とポート) を与える。
// port-forward は、サーバが接続を切った際の RST で終了するため用いない。集約層を経由した
// 要求が受理されることは、CI が kubectl get --raw で確かめる。
func TestWebhookRequiresClientCertificate(t *testing.T) {
	addr := os.Getenv("E2E_WEBHOOK_ADDR")
	if addr == "" {
		t.Skip("E2E_WEBHOOK_ADDR が必要")
	}
	other := selfSignedClientCert(t)
	cases := []struct {
		name  string
		certs []tls.Certificate
	}{
		{"証明書なし", nil},
		{"集約層向け以外の CA", []tls.Certificate{other}},
	}
	for _, c := range cases {
		for _, v := range []uint16{tls.VersionTLS12, tls.VersionTLS13} {
			t.Run(c.name+"/"+tls.VersionName(v), func(t *testing.T) {
				resp, err := probe(addr, v, c.certs)
				if err == nil || !strings.Contains(err.Error(), "remote error: tls:") {
					t.Fatalf("TLS ハンドシェイクで拒否されていない: err=%v resp=%q", err, resp)
				}
				if strings.HasPrefix(resp, "HTTP/") {
					t.Fatalf("要求が処理された: %q", resp)
				}
			})
		}
	}
}

// probe は addr へ TLS でつなぎ HTTP/1.1 の要求を送り、得られた応答の先頭を返す。
func probe(addr string, version uint16, certs []tls.Certificate) (string, error) {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr, &tls.Config{
		// サーバ証明書は検証の対象外である。確かめるのはサーバがクライアントを拒否することである。
		InsecureSkipVerify: true, //nolint:gosec // G402: 検証するのはクライアント証明書の拒否であり、サーバの真正性ではない
		Certificates:       certs,
		MinVersion:         version,
		MaxVersion:         version,
		NextProtos:         []string{"http/1.1"},
	})
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.WriteString(conn, "GET /apis/acme.dns-platform.jp/v1alpha1 HTTP/1.1\r\nHost: webhook\r\nConnection: close\r\n\r\n"); err != nil {
		return "", err
	}
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	return string(buf[:n]), err
}

// selfSignedClientCert は、クラスタと無関係な CA で署名したクライアント証明書を返す。
func selfSignedClientCert(t *testing.T) tls.Certificate {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "e2e-other-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// 集約層の証明書と同じ名前を名乗っても、署名者が異なれば拒否される。
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "front-proxy-client"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
