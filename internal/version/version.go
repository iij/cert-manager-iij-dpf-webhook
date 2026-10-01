// SPDX-License-Identifier: Apache-2.0

// Package version はビルド時に埋め込むバージョン情報を保持する。
//
//	go build -ldflags "-X github.com/iij/cert-manager-iij-dpf-webhook/internal/version.Version=v0.1.0 \
//	  -X github.com/iij/cert-manager-iij-dpf-webhook/internal/version.Revision=$(git rev-parse --short HEAD)"
package version

// ビルド時に -ldflags -X で上書きする。
var (
	Version  = "dev"
	Revision = "unknown"
)

// String は "<Version> (<Revision>)" を返す。
func String() string {
	return Version + " (" + Revision + ")"
}
