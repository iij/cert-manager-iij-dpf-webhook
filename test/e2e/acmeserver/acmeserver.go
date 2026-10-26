// SPDX-License-Identifier: Apache-2.0

// Package acmeserver は e2e が用いる ACME サーバの接続先を決める
// (憲章「ACME サーバの利用」/ research.md D-17)。
//
// 本プロジェクトのテストと CI では本番環境の ACME サーバを用いない。許可リストに無い
// 接続先はすべて拒否し、未設定の場合はステージング環境とする (本番環境へ既定で接続しない)。
package acmeserver

import (
	"errors"
	"fmt"
	"slices"
)

// EnvACMEServer は接続先を与える環境変数である。
const EnvACMEServer = "ACME_SERVER"

// Staging は Let's Encrypt のステージング環境であり、未設定時の既定である。
const Staging = "https://acme-staging-v02.api.letsencrypt.org/directory"

// ErrNotAllowed は許可リストに無い接続先が与えられた場合に返される。
var ErrNotAllowed = errors.New("acmeserver: ACME server is not allowed in tests and CI")

// allowed はテストと CI が用いてよい接続先である (plan.md の Technical Context)。
var allowed = []string{Staging}

// production は本番環境の ACME サーバである。拒否の理由を示すために持つ。
var production = []string{
	"https://acme-v02.api.letsencrypt.org/directory",
	"https://dv.acme-v02.api.pki.goog/directory",
	"https://acme.zerossl.com/v2/DV90",
	"https://api.buypass.com/acme/directory",
}

// Resolve は環境変数から接続先を決める。
func Resolve(getenv func(string) string) (string, error) {
	v := getenv(EnvACMEServer)
	if v == "" {
		return Staging, nil
	}
	if slices.Contains(allowed, v) {
		return v, nil
	}
	if slices.Contains(production, v) {
		return "", fmt.Errorf("%w: %s is a production ACME server", ErrNotAllowed, v)
	}
	return "", fmt.Errorf("%w: %s is not in the allow list", ErrNotAllowed, v)
}
