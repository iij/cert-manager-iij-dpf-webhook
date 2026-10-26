// SPDX-License-Identifier: Apache-2.0

package tokensource_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iij/dpf-go/misc/k8s"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/tokensource"
)

const secretValue = "s3cr3t-token-value"

// fakeSecrets は Secret の読み出しを記録する。
type fakeSecrets struct {
	calls  []string
	secret *corev1.Secret
	err    error
}

func (f *fakeSecrets) Get(_ context.Context, namespace, name string, _ metav1.GetOptions) (*corev1.Secret, error) {
	f.calls = append(f.calls, namespace+"/"+name)
	if f.err != nil {
		return nil, f.err
	}
	return f.secret, nil
}

var _ k8s.SecretsAPI = (*fakeSecrets)(nil)

func secret(data map[string]string) *corev1.Secret {
	s := &corev1.Secret{Data: map[string][]byte{}}
	for k, v := range data {
		s.Data[k] = []byte(v)
	}
	return s
}

// D-09: namespace・Secret 名・key を明示的に渡し、呼び出しのたびに読む。
func TestProviderReadsExplicitSource(t *testing.T) {
	f := &fakeSecrets{secret: secret(map[string]string{"tok": secretValue})}
	p, err := tokensource.New(f).Provider("team-a", "my-secret", "tok")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		got, err := p(context.Background())
		if err != nil || got != secretValue {
			t.Fatalf("token = %q, %v", got, err)
		}
	}
	if len(f.calls) != 2 || f.calls[0] != "team-a/my-secret" {
		t.Fatalf("calls = %v", f.calls)
	}
}

// namespace を推測しない。
func TestProviderRequiresNamespace(t *testing.T) {
	if _, err := tokensource.New(&fakeSecrets{}).Provider("", "dpf-token", "token"); err == nil {
		t.Fatal("empty namespace was accepted")
	}
}

// FR-008 / FR-012: 失敗を判別でき、エラーに Secret の値を含めない。
func TestProviderErrors(t *testing.T) {
	gr := schema.GroupResource{Resource: "secrets"}
	cases := []struct {
		name string
		f    *fakeSecrets
		want error
	}{
		{"Secret 不在", &fakeSecrets{err: apierrors.NewNotFound(gr, "dpf-token")}, k8s.ErrSecretNotFound},
		{"key 不在", &fakeSecrets{secret: secret(map[string]string{"other": secretValue})}, k8s.ErrKeyNotFound},
		{"値が空", &fakeSecrets{secret: secret(map[string]string{"token": ""})}, k8s.ErrEmptySecret},
		{"権限なし", &fakeSecrets{err: apierrors.NewForbidden(gr, "dpf-token", errors.New("no"))}, k8s.ErrForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := tokensource.New(c.f).Provider("ns", "dpf-token", "token")
			if err != nil {
				t.Fatal(err)
			}
			_, err = p(context.Background())
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if strings.Contains(err.Error(), secretValue) {
				t.Errorf("error leaks the secret value: %v", err)
			}
		})
	}
}

// errUnreachable は Kubernetes API への到達失敗を表す。
var errUnreachable = errors.New("dial tcp 10.0.0.1:443: connect: connection refused")

// flakySecrets は最初の fails 回だけ到達失敗を返す。
type flakySecrets struct {
	fakeSecrets
	fails int
}

func (f *flakySecrets) Get(ctx context.Context, namespace, name string, o metav1.GetOptions) (*corev1.Secret, error) {
	if f.fails > 0 {
		f.fails--
		f.calls = append(f.calls, namespace+"/"+name)
		return nil, errUnreachable
	}
	return f.fakeSecrets.Get(ctx, namespace, name, o)
}

// D-09 / FR-008 / FR-009: misc/k8s のエラー型で恒久的／一時的を分ける。
func TestIsPermanent(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{k8s.ErrSecretNotFound, true},
		{k8s.ErrKeyNotFound, true},
		{k8s.ErrEmptySecret, true},
		{k8s.ErrForbidden, true},
		{k8s.ErrNoCredentials, true},
		{k8s.ErrNamespaceUnknown, true},
		{k8s.ErrNilClient, true},
		{tokensource.ErrNamespaceRequired, true},
		{errUnreachable, false},
		{context.DeadlineExceeded, false},
	}
	for _, c := range cases {
		if got := tokensource.IsPermanent(c.err); got != c.want {
			t.Errorf("IsPermanent(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

// FR-008: 恒久的な失敗は再試行しない。
func TestProviderDoesNotRetryPermanentFailures(t *testing.T) {
	gr := schema.GroupResource{Resource: "secrets"}
	f := &fakeSecrets{err: apierrors.NewNotFound(gr, "dpf-token")}
	p, err := tokensource.New(f, tokensource.WithRetry(5, 0)).Provider("ns", "dpf-token", "token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p(context.Background()); !tokensource.IsPermanent(err) {
		t.Fatalf("err = %v, want permanent", err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("Get was called %d times, want 1", len(f.calls))
	}
}

// FR-009: 到達失敗は一時的な失敗として、上限の回数まで再試行する。
func TestProviderRetriesTemporaryFailuresUpToLimit(t *testing.T) {
	t.Run("上限に達して失敗", func(t *testing.T) {
		f := &flakySecrets{fails: 100}
		p, err := tokensource.New(f, tokensource.WithRetry(3, 0)).Provider("ns", "dpf-token", "token")
		if err != nil {
			t.Fatal(err)
		}
		_, err = p(context.Background())
		if err == nil || tokensource.IsPermanent(err) || !errors.Is(err, errUnreachable) {
			t.Fatalf("err = %v, want temporary", err)
		}
		if len(f.calls) != 3 {
			t.Fatalf("Get was called %d times, want 3", len(f.calls))
		}
	})
	t.Run("再試行で回復", func(t *testing.T) {
		f := &flakySecrets{fakeSecrets: fakeSecrets{secret: secret(map[string]string{"token": secretValue})}, fails: 2}
		p, err := tokensource.New(f, tokensource.WithRetry(3, 0)).Provider("ns", "dpf-token", "token")
		if err != nil {
			t.Fatal(err)
		}
		if got, err := p(context.Background()); err != nil || got != secretValue {
			t.Fatalf("token = %q, %v", got, err)
		}
		if len(f.calls) != 3 {
			t.Fatalf("Get was called %d times, want 3", len(f.calls))
		}
	})
	t.Run("既定にも上限がある", func(t *testing.T) {
		f := &flakySecrets{fails: 1000}
		p, err := tokensource.New(f, tokensource.WithRetry(0, 0)).Provider("ns", "dpf-token", "token")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p(context.Background()); err == nil {
			t.Fatal("unbounded retry succeeded")
		}
		if n := len(f.calls); n != tokensource.DefaultMaxAttempts {
			t.Fatalf("Get was called %d times, want %d", n, tokensource.DefaultMaxAttempts)
		}
	})
}

// 再試行の待ちは context の期限を越えない。
func TestProviderRetryStopsAtContextDone(t *testing.T) {
	f := &flakySecrets{fails: 100}
	p, err := tokensource.New(f, tokensource.WithRetry(10, time.Hour)).Provider("ns", "dpf-token", "token")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := p(ctx); err == nil {
		t.Fatal("succeeded")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("retry waited %v beyond the context deadline", d)
	}
	if len(f.calls) != 1 {
		t.Fatalf("Get was called %d times, want 1", len(f.calls))
	}
}
