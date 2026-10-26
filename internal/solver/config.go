// SPDX-License-Identifier: Apache-2.0

package solver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/zonefilter"
)

// solver 設定の既定値 (FR-003)。
const (
	DefaultEndpoint   = "https://api.dns-platform.jp/dpf/v1"
	DefaultSecretName = "dpf-token"
	DefaultSecretKey  = "token"
)

var (
	// ErrInvalidConfig は solver 設定を解釈できない場合に返される。既定値へフォールバックしない (FR-005)。
	ErrInvalidConfig = errors.New("invalid solver config")
	// ErrNamespaceNotAllowed はトークンを読む namespace が許されない場合に返される (FR-007)。
	ErrNamespaceNotAllowed = errors.New("token secret namespace is not allowed for this issuer")
)

// rawConfig は Issuer / ClusterIssuer の dns01.webhook.config である
// (contracts/solver-config.schema.json)。
type rawConfig struct {
	Endpoint       string `json:"endpoint"`
	TokenSecretRef *struct {
		Name      string `json:"name"`
		Key       string `json:"key"`
		Namespace string `json:"namespace"`
	} `json:"tokenSecretRef"`
	DomainFilters []string `json:"domainFilters"`
}

// config は解釈済みの solver 設定である。
type config struct {
	Endpoint        string
	SecretName      string
	SecretKey       string
	SecretNamespace string
	Filter          zonefilter.Filter
}

// parseConfig は solver 設定を解釈する。未知のフィールドは拒否する (FR-004)。
func parseConfig(in *apiextensionsv1.JSON) (config, error) {
	var rc rawConfig
	if in != nil && len(in.Raw) > 0 {
		dec := json.NewDecoder(bytes.NewReader(in.Raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&rc); err != nil {
			return config{}, fmt.Errorf("%w: %v", ErrInvalidConfig, err) //nolint:errorlint // 解析の詳細はメッセージとしてのみ残す
		}
	}
	c := config{Endpoint: rc.Endpoint, SecretName: DefaultSecretName, SecretKey: DefaultSecretKey}
	if c.Endpoint == "" {
		c.Endpoint = DefaultEndpoint
	}
	if u, err := url.Parse(c.Endpoint); err != nil || u.Scheme != "https" || u.Host == "" {
		return config{}, fmt.Errorf("%w: endpoint must be an absolute https URL: %q", ErrInvalidConfig, c.Endpoint)
	}
	if ref := rc.TokenSecretRef; ref != nil {
		if ref.Name != "" {
			c.SecretName = ref.Name
		}
		if ref.Key != "" {
			c.SecretKey = ref.Key
		}
		c.SecretNamespace = ref.Namespace
	}
	f, err := zonefilter.Parse(rc.DomainFilters)
	if err != nil {
		return config{}, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	c.Filter = f
	return c, nil
}

// Warnings は既定の拒否を緩める設定に対する警告を返す (FR-023a / 原則 VI)。
func (c config) Warnings() []string {
	if c.Filter.AllowsAllZones() {
		return []string{"domainFilters contains \"*\": every zone reachable with the token may be modified"}
	}
	return nil
}

// secretNamespace はトークンを読む namespace を決める (FR-006 / FR-007)。
//
//   - ClusterIssuer 由来 (ambient = true): tokenSecretRef.namespace、省略時は本 Pod の namespace
//   - namespace 限定の Issuer 由来: ResourceNamespace のみ。tokenSecretRef.namespace が
//     それ以外を指していれば失敗させ、読み替えない
func (c config) secretNamespace(ambient bool, resourceNamespace, podNamespace string) (string, error) {
	if ambient {
		if c.SecretNamespace != "" {
			return c.SecretNamespace, nil
		}
		if podNamespace == "" {
			return "", fmt.Errorf("%w: pod namespace is unknown", ErrNamespaceNotAllowed)
		}
		return podNamespace, nil
	}
	if resourceNamespace == "" {
		return "", fmt.Errorf("%w: resource namespace is empty", ErrNamespaceNotAllowed)
	}
	if c.SecretNamespace != "" && c.SecretNamespace != resourceNamespace {
		return "", fmt.Errorf("%w: issuer in %q refers to namespace %q",
			ErrNamespaceNotAllowed, resourceNamespace, c.SecretNamespace)
	}
	return resourceNamespace, nil
}
