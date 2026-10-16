// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"strings"
	"testing"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/solver"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/testutil"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/tokensource"
)

// secretsFor は 1 つの Secret だけを持つ Kubernetes API を模す。
type secretsFor struct {
	data map[string]string // nil なら Secret が無い
}

func (s secretsFor) Get(_ context.Context, _, name string, _ metav1.GetOptions) (*corev1.Secret, error) {
	if s.data == nil {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, name)
	}
	sec := &corev1.Secret{Data: map[string][]byte{}}
	for k, v := range s.data {
		sec.Data[k] = []byte(v)
	}
	return sec, nil
}

// T100 / SC-009: 5 種類の失敗 (無効なトークン / Secret 不在 / key 不在 / ゾーン不在 /
// DPF 到達不能) を、ログのみで判別できる。各失敗のログには固有の目印が現れ、他の失敗の目印は
// 現れない。
func TestUS3DiagnosisFromLogs(t *testing.T) {
	e := loadEnv(t)
	markers := map[string]string{
		// DPF は無効なトークンに 401 ではなく 400 ParameterError (attribute access_token) を返す
		// (DPF-API リファレンス 2.3)。
		"無効なトークン":   "access_token",
		"Secret 不在": dpf.ErrSecretNotFound.Error(),
		"key 不在":    dpf.ErrSecretKeyNotFound.Error(),
		"ゾーン不在":     dpf.ErrZoneNotFound.Error(),
		"DPF 到達不能":  dpf.ErrUnavailable.Error(),
	}
	type scenario struct {
		tokens   dpf.TokenProviders
		endpoint string
		zone     string
	}
	label := "_acme-challenge.e2e-us3"
	scenarios := map[string]scenario{
		"無効なトークン":   {tokens: testutil.FixedToken("invalid-token-for-e2e")},
		"Secret 不在": {tokens: tokensource.New(secretsFor{})},
		"key 不在":    {tokens: tokensource.New(secretsFor{data: map[string]string{"other": e.token}})},
		"ゾーン不在":     {tokens: testutil.FixedToken(e.token), zone: "e2e-missing-zone." + e.zone.String()},
		"DPF 到達不能":  {tokens: testutil.FixedToken(e.token), endpoint: "https://127.0.0.1:1/dpf/v1"},
	}
	for name, sc := range scenarios {
		t.Run(name, func(t *testing.T) {
			var logs testutil.LogBuffer
			o := solver.DefaultOptions()
			o.LockOwner = "e2e-us3"
			o.PodNamespace = "e2e"
			s := solver.NewWithDependencies(o, logs.Logger(), sc.tokens,
				dpf.NewSharedTransport(http.DefaultTransport, dpf.DefaultRateLimit, dpf.DefaultBurst, dpf.DefaultMaxConcurrency))
			r := e.request(whapi.ChallengeActionPresent, label, "us3")
			if sc.zone != "" {
				r.ResolvedZone = sc.zone
				r.ResolvedFQDN = label + "." + sc.zone
			}
			if sc.endpoint != "" {
				r.Config.Raw = []byte(strings.Replace(e.config(), `"endpoint":"`+endpointOf(e)+`"`,
					`"endpoint":"`+sc.endpoint+`"`, 1))
			}
			if err := s.Present(r); err == nil {
				t.Fatal("Present succeeded")
			}
			out := logs.String()
			if !strings.Contains(out, markers[name]) {
				t.Errorf("log does not contain %q:\n%s", markers[name], out)
			}
			for other, m := range markers {
				if other != name && strings.Contains(out, m) {
					t.Errorf("log also contains the marker of %s (%q)", other, m)
				}
			}
			if !strings.Contains(out, "failure=") {
				t.Errorf("log does not record the failure classification")
			}
			// トークンの誤りは Secret を直さない限り解消しない (research.md D-23)。
			if name == "無効なトークン" && !strings.Contains(out, "failure=permanent") {
				t.Errorf("invalid token is not classified as permanent:\n%s", out)
			}
		})
	}
}

func endpointOf(e *env) string {
	if e.endpoint != "" {
		return e.endpoint
	}
	return solver.DefaultEndpoint
}
