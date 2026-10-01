// SPDX-License-Identifier: Apache-2.0

// Package tokensource は DPF アクセストークンを Kubernetes の Secret から取得する。
//
// 取得は github.com/iij/dpf-go/misc/k8s に委ね、自前で Secret を読まない (D-09)。
// namespace は要求ごとに解決した値を必ず明示的に渡し、推測しない。
package tokensource

import (
	"context"
	"errors"

	"github.com/iij/dpf-go/misc/k8s"
)

// ErrNamespaceRequired は namespace が空の場合に返される。
var ErrNamespaceRequired = errors.New("tokensource: namespace is required")

// Factory は Secret の出所からトークンを返す関数を作る。
type Factory struct {
	secrets k8s.SecretsAPI
}

// New は Factory を返す。
func New(secrets k8s.SecretsAPI) *Factory {
	return &Factory{secrets: secrets}
}

// Provider は namespace / name / key の Secret から、呼び出しのたびにトークンを読む関数を
// 返す。外部でのローテーションに再起動なしで追随する (FR-011)。
func (f *Factory) Provider(namespace, name, key string) (func(context.Context) (string, error), error) {
	if namespace == "" {
		return nil, ErrNamespaceRequired
	}
	return k8s.NewTokenProvider(f.secrets,
		k8s.WithNamespace(namespace), k8s.WithSecretName(name), k8s.WithKey(key))
}
