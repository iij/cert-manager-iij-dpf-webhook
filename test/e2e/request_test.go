// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package e2e

import (
	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// request は検証用ゾーンに対するチャレンジ要求である。
func (e *env) request(action whapi.ChallengeAction, label, key string) *whapi.ChallengeRequest {
	return &whapi.ChallengeRequest{
		UID: "e2e", Action: action, Type: "dns-01", DNSName: e.zone.String(), Key: key,
		ResourceNamespace: "e2e", ResolvedFQDN: label + "." + e.zone.String(), ResolvedZone: e.zone.String(),
		AllowAmbientCredentials: true, Config: &apiextensionsv1.JSON{Raw: []byte(e.config())},
	}
}
