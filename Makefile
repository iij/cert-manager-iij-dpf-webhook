# SPDX-License-Identifier: Apache-2.0

# ツールの版は本ファイルが唯一の出典である。CI は本ファイルのターゲットを経由して
# 実行し、自前で版を持たない (憲章「開発ワークフローと品質ゲート」)。
# Go の版は go.mod の toolchain が唯一の出典である。
GOLANGCI_LINT_VERSION ?= v2.13.2
GOVULNCHECK_VERSION   ?= v1.7.0
GO_LICENSES_VERSION   ?= v2.0.1
BETTERLEAKS_VERSION   ?= v1.9.0

GO          ?= go
GOLANGCI    ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
GOVULNCHECK ?= $(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
GO_LICENSES ?= $(GO) run github.com/google/go-licenses/v2@$(GO_LICENSES_VERSION)
BETTERLEAKS ?= $(GO) run github.com/betterleaks/betterleaks@$(BETTERLEAKS_VERSION)

# 依存ライセンスの許容リスト (憲章「ライセンス」)。
ALLOWED_LICENSES := Apache-2.0,MIT,BSD-2-Clause,BSD-3-Clause,ISC,MPL-2.0

.PHONY: all fmt fmt-check lint vuln test contract build spdx domain-string acme-endpoints changelog-test licenses secrets check

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
