# SPDX-License-Identifier: Apache-2.0

# ツールの版は本ファイルが唯一の出典である。CI は本ファイルのターゲットを経由して
# 実行し、自前で版を持たない (憲章「開発ワークフローと品質ゲート」)。
# Go の版は go.mod の toolchain が唯一の出典である。
GOLANGCI_LINT_VERSION ?= v2.13.2
GOVULNCHECK_VERSION   ?= v1.7.0
GO_LICENSES_VERSION   ?= v2.0.1
BETTERLEAKS_VERSION   ?= v1.9.0
TRIVY_VERSION         ?= 0.75.0

GO          ?= go
GOLANGCI    ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
GOVULNCHECK ?= $(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
GO_LICENSES ?= $(GO) run github.com/google/go-licenses/v2@$(GO_LICENSES_VERSION)
BETTERLEAKS ?= $(GO) run github.com/betterleaks/betterleaks@$(BETTERLEAKS_VERSION)

VERSION  ?= dev
REVISION ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
VERSION_PKG := github.com/iij/cert-manager-iij-dpf-webhook/internal/version

# 配布するバイナリのビルド (憲章「コンテナイメージとチャートの既定拒否」/ research.md D-21)。
# ASLR を有効にするため位置独立 (-buildmode=pie) とし、scratch で起動できるよう静的にリンクする
# (static-pie)。Go の内部リンカは static-pie を作れないため外部リンカを用い、C ライブラリは
# musl (MIT) とする。glibc (LGPL) を静的にリンクしない。musl のビルダ (Alpine) で実行する。
RELEASE_LDFLAGS := -s -w -linkmode=external -extldflags=-static-pie \
	-X $(VERSION_PKG).Version=$(VERSION) -X $(VERSION_PKG).Revision=$(REVISION)

# 依存ライセンスの許容リスト (憲章「ライセンス」)。
ALLOWED_LICENSES := Apache-2.0,MIT,BSD-2-Clause,BSD-3-Clause,ISC,MPL-2.0

.PHONY: release-binary third-party-licenses image check-image scan-image all fmt fmt-check lint vuln test contract build spdx domain-string acme-endpoints changelog-test licenses secrets check

all: check build

fmt:
	gofmt -w $$(git ls-files '*.go')

fmt-check:
	@out=$$(gofmt -l $$(git ls-files '*.go')); if [ -n "$$out" ]; then echo "$$out"; exit 1; fi

lint:
	$(GOLANGCI) run ./...

vuln:
	$(GOVULNCHECK) ./...

test:
	$(GO) test ./...

release-binary:
	CGO_ENABLED=1 $(GO) build -trimpath -buildmode=pie -tags netgo,osusergo \
		-ldflags '$(RELEASE_LDFLAGS)' -o dist/webhook ./cmd/webhook

# 依存のライセンス本文を、モジュールパスの階層を保って dist/licenses/third-party に置く。
# ソースの提供を求めるライセンス (MPL-2.0 など) の依存はソースも置かれる (go-licenses save)。
third-party-licenses:
	rm -rf dist/licenses
	$(GO_LICENSES) save ./cmd/webhook --save_path=dist/licenses/third-party
	mkdir -p dist/licenses/third-party/std dist/licenses/third-party/musl
	cp "$$($(GO) env GOROOT)/LICENSE" dist/licenses/third-party/std/LICENSE
	cp third_party/musl/COPYRIGHT dist/licenses/third-party/musl/COPYRIGHT

IMAGE ?= cert-manager-iij-dpf-webhook:dev
image:
	docker build --build-arg VERSION=$(VERSION) --build-arg REVISION=$(REVISION) \
		--build-arg CREATED=$$(date -u +%Y-%m-%dT%H:%M:%SZ) -t $(IMAGE) .

check-image:
	./scripts/check-image.sh $(IMAGE)

# イメージの脆弱性スキャン。修正版のある HIGH / CRITICAL を検出したら失敗する。
scan-image:
	docker run --rm -v /var/run/docker.sock:/var/run/docker.sock aquasec/trivy:$(TRIVY_VERSION) \
		image --no-progress --exit-code 1 --severity HIGH,CRITICAL --ignore-unfixed $(IMAGE)

# cert-manager webhook API の契約テスト。
contract:
	$(GO) test -count=1 ./test/contract/

build:
	$(GO) build ./...

spdx:
	./scripts/check-spdx.sh

domain-string:
	./scripts/check-domain-string.sh

acme-endpoints:
	./scripts/check-acme-endpoints_test.sh
	./scripts/check-acme-endpoints.sh

changelog-test:
	./scripts/check-changelog_test.sh

# 許容リスト外のライセンスの依存がないことを検査する。
licenses:
	$(GO_LICENSES) check ./cmd/webhook --allowed_licenses=$(ALLOWED_LICENSES)

# 履歴を含めたシークレットの混入を検査する。誤検出の除外は .betterleaksignore に理由とともに書く。
secrets:
	$(BETTERLEAKS) git . --redact

check: fmt-check spdx domain-string acme-endpoints changelog-test secrets licenses lint test
