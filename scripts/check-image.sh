#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# 配布するコンテナイメージを検査する (憲章「配布」「コンテナイメージとチャートの既定拒否」)。
#
# 使い方: check-image.sh <イメージ>
#   - scratch であること (シェル・パッケージマネージャ・実行に不要なファイルが無い)
#   - バイナリが位置独立 (PIE) かつ静的にリンクされていること (ASLR。scratch で起動できる)
#   - バイナリが CA 証明書 (golang.org/x/crypto/x509roots/fallback) を備えること
#   - /licenses/ に本体 (LICENSE / NOTICE) と、依存のライセンス本文がモジュールパスの階層で
#     置かれていること。単一ファイルへ連結していないこと
#   - OCI の標準アノテーション (ラベル) が付いており、licenses が Apache-2.0 であること
#   - 非 root の固定数値 UID で実行されること
set -euo pipefail
export LC_ALL=C # readelf などの出力をロケールに依らず照合する

if [[ $# -ne 1 ]]; then
	echo "usage: $0 <image>" >&2
	exit 2
fi
image="$1"
engine="${CONTAINER_ENGINE:-docker}"
work="$(mktemp -d)"
trap 'rm -rf "$work"; [[ -n "${cid:-}" ]] && "$engine" rm -f "$cid" >/dev/null 2>&1 || true' EXIT

fail=0
ng() {
	echo "NG: $*" >&2
	fail=1
}

cid="$("$engine" create "$image" /webhook)"
"$engine" export "$cid" >"$work/fs.tar"
mkdir "$work/fs"
tar -xf "$work/fs.tar" -C "$work/fs"

# scratch: 許すのは本サービスのバイナリと /licenses/ のみ (エンジンが作るファイルを除く)。
while IFS= read -r p; do
	case "$p" in
	./webhook | ./licenses | ./licenses/*) ;;
	./etc | ./etc/hosts | ./etc/hostname | ./etc/resolv.conf | ./etc/mtab | ./dev | ./dev/* | ./proc | ./sys | ./.dockerenv) ;;
	*) ng "unexpected file in the image: $p" ;;
	esac
done < <(cd "$work/fs" && find . -mindepth 1)
for p in bin/sh bin/busybox sbin/apk usr/bin/apt; do
	[[ -e "$work/fs/$p" ]] && ng "$p exists"
done

# ASLR: 位置独立 (ELF の型が DYN) かつ静的 (プログラムインタプリタが無い)。
bin="$work/fs/webhook"
if ! readelf -h "$bin" | grep -Eq 'Type:[[:space:]]+DYN'; then
	ng "the binary is not position independent (ELF type is not DYN)"
fi
if readelf -l "$bin" | grep -q 'INTERP'; then
	ng "the binary is dynamically linked (it has a program interpreter)"
fi

# CA 証明書: scratch には証明書ストアが無いため、バイナリが備える。
if ! go version -m "$bin" | grep -q 'golang.org/x/crypto/x509roots/fallback'; then
	ng "the binary does not embed CA certificates (golang.org/x/crypto/x509roots/fallback)"
fi

# ライセンス。
lic="$work/fs/licenses"
for f in LICENSE NOTICE; do
	[[ -s "$lic/$f" ]] || ng "/licenses/$f is missing"
done
[[ -d "$lic/third-party" ]] || ng "/licenses/third-party is missing"
# 依存モジュールごとにライセンス本文がモジュールパスの階層で置かれていること。
while IFS= read -r mod; do
	if ! compgen -G "$lic/third-party/$mod/*" >/dev/null &&
		! find "$lic/third-party" -path "$lic/third-party/$mod*" -type f -print -quit | grep -q .; then
		ng "license of $mod is missing under /licenses/third-party"
	fi
done < <(go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}{{end}}{{end}}' ./cmd/webhook | sort -u)
# 静的にリンクされる Go の標準ライブラリと C ライブラリ (musl)。
[[ -s "$lic/third-party/std/LICENSE" ]] || ng "/licenses/third-party/std/LICENSE (Go standard library) is missing"
[[ -s "$lic/third-party/musl/COPYRIGHT" ]] || ng "/licenses/third-party/musl/COPYRIGHT is missing"
# 単一ファイルへの連結をしない (third-party の直下にファイルを置かない)。
if find "$lic/third-party" -maxdepth 1 -type f | grep -q .; then
	ng "/licenses/third-party has files at its top level (licenses must not be concatenated)"
fi

# OCI の標準アノテーション。
labels="$("$engine" image inspect "$image" --format '{{json .Config.Labels}}')"
for k in licenses title description vendor source version revision created; do
	if ! grep -q "\"org.opencontainers.image.$k\":\"[^\"]" <<<"$labels"; then
		ng "label org.opencontainers.image.$k is missing or empty"
	fi
done
grep -q '"org.opencontainers.image.licenses":"Apache-2.0"' <<<"$labels" ||
	ng "org.opencontainers.image.licenses is not Apache-2.0"

# 非 root の固定数値 UID。
user="$("$engine" image inspect "$image" --format '{{.Config.User}}')"
if [[ ! "$user" =~ ^[1-9][0-9]*(:[0-9]+)?$ ]]; then
	ng "the image user is not a fixed non-root numeric UID: '$user'"
fi

if [[ "$fail" -eq 0 ]]; then
	echo "ok: $image"
fi
exit "$fail"
