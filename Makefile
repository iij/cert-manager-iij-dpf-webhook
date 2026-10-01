# SPDX-License-Identifier: Apache-2.0

# ツールの版は本ファイルが唯一の出典である。CI は本ファイルのターゲットを経由して
# 実行し、自前で版を持たない (憲章「開発ワークフローと品質ゲート」)。
GOLANGCI_LINT_VERSION ?= v2.13.2
GOVULNCHECK_VERSION   ?= v1.7.0

GO          ?= go
GOLANGCI    ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
GOVULNCHECK ?= $(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

.PHONY: all fmt fmt-check lint vuln test build spdx domain-string acme-endpoints check

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

build:
	$(GO) build ./...

spdx:
	./scripts/check-spdx.sh

domain-string:
	./scripts/check-domain-string.sh

acme-endpoints:
	./scripts/check-acme-endpoints_test.sh
	./scripts/check-acme-endpoints.sh

check: fmt-check spdx domain-string acme-endpoints lint test
