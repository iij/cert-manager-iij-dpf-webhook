// SPDX-License-Identifier: Apache-2.0

package tokensource_test

import (
	"context"
	"errors"
	"strings"
	"testing"

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
