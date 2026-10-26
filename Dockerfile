# syntax=docker/dockerfile:1
# SPDX-License-Identifier: Apache-2.0
#
# 配布イメージ (憲章「配布」「コンテナイメージとチャートの既定拒否」/ research.md D-21)。
# - ベースは scratch。シェル・パッケージマネージャを含めない
# - バイナリは static-pie (ASLR)。C ライブラリは musl (MIT) を静的にリンクする
# - CA 証明書はバイナリが備える (golang.org/x/crypto/x509roots/fallback)
# - /licenses/ に本体と依存のライセンス本文を、モジュールパスの階層を保って置く

# ビルダ。Go の版は go.mod の toolchain に合わせる。musl の版は third_party/musl の本文と一致させる。
ARG GO_IMAGE=golang:1.27.1-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414

FROM ${GO_IMAGE} AS build
ARG MUSL_VERSION=1.2.6
RUN apk add --no-cache gcc musl-dev make git \
	&& musl="$(apk info -e -v musl | sed -n 's/^musl-\([0-9.]*\)-r[0-9]*$/\1/p')" \
	&& if [ "$musl" != "$MUSL_VERSION" ]; then \
		echo "musl $musl does not match the bundled license text (MUSL_VERSION=$MUSL_VERSION)" >&2; exit 1; \
	fi
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG REVISION=unknown
RUN make release-binary third-party-licenses VERSION="$VERSION" REVISION="$REVISION" \
	&& mkdir -p dist/licenses \
	&& cp LICENSE NOTICE dist/licenses/

FROM scratch
ARG VERSION=dev
ARG REVISION=unknown
ARG CREATED=unknown
LABEL org.opencontainers.image.title="cert-manager-iij-dpf-webhook" \
	org.opencontainers.image.description="cert-manager DNS01 solver for IIJ DNS Platform Service (DPF)" \
	org.opencontainers.image.vendor="Internet Initiative Japan Inc." \
	org.opencontainers.image.source="https://github.com/iij/cert-manager-iij-dpf-webhook" \
	org.opencontainers.image.licenses="Apache-2.0" \
	org.opencontainers.image.version="${VERSION}" \
	org.opencontainers.image.revision="${REVISION}" \
	org.opencontainers.image.created="${CREATED}"
COPY --from=build /src/dist/webhook /webhook
COPY --from=build /src/dist/licenses /licenses
USER 65532:65532
ENTRYPOINT ["/webhook"]
