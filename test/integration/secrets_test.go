// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"sync"

	"github.com/iij/dpf-go/misc/k8s"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// fakeSecrets は Kubernetes API の Secret の読み出しを模す。値は namespace/name をキーにする。
type fakeSecrets struct {
	mu      sync.Mutex
	secrets map[string]map[string]string
	// unreachable は Kubernetes API へ到達できない状態を表す。
	unreachable bool
	calls       int
}

var _ k8s.SecretsAPI = (*fakeSecrets)(nil)

func (f *fakeSecrets) Get(_ context.Context, namespace, name string, _ metav1.GetOptions) (*corev1.Secret, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.unreachable {
		return nil, errUnreachable
	}
	data, ok := f.secrets[namespace+"/"+name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, name)
	}
	s := &corev1.Secret{Data: map[string][]byte{}}
	for k, v := range data {
		s.Data[k] = []byte(v)
	}
	return s, nil
}

func (f *fakeSecrets) set(namespace, name, key, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.secrets == nil {
		f.secrets = map[string]map[string]string{}
	}
	f.secrets[namespace+"/"+name] = map[string]string{key: value}
}

func (f *fakeSecrets) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type unreachableError struct{}

func (unreachableError) Error() string { return "dial tcp 10.96.0.1:443: connect: connection refused" }

var errUnreachable error = unreachableError{}
