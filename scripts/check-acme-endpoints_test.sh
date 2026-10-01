#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# scripts/check-acme-endpoints.sh のテスト (憲章「ACME サーバの利用」/ research.md D-17)。
set -uo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
check="$here/check-acme-endpoints.sh"
data="$here/testdata/acme-endpoints"
fail=0

# run <名前> <期待する終了コード> <出力に含まれるべき文字列 (空なら検査しない)> -- <引数...>
run() {
	local name="$1" want="$2" needle="$3"
	shift 4
	local out code
	out="$("$check" "$@" 2>&1)"
	code=$?
	if [[ "$code" != "$want" ]]; then
		echo "FAIL: $name: 終了コード $code (期待 $want)"
		echo "$out" | sed 's/^/    /'
		fail=1
		return
	fi
	if [[ -n "$needle" && "$out" != *"$needle"* ]]; then
		echo "FAIL: $name: 出力に「$needle」が無い"
		echo "$out" | sed 's/^/    /'
		fail=1
		return
	fi
	echo "ok:   $name"
}

run "ステージング環境のみなら成功する" 0 "" -- --root "$data/staging-only"
run "本番環境の接続先を拒否し、本番環境であることを示す" 1 "https://acme-v02.api.letsencrypt.org/directory" -- --root "$data/production"
run "本番環境の拒否の理由に本番環境であることを示す" 1 "本番環境" -- --root "$data/production"
run "一覧に無い未知の ACME サーバも拒否する" 1 "https://acme.example-ca.test/acme/directory" -- --root "$data/unknown"
run "利用者向けの文書 (README・quickstart) は検査しない" 0 "" -- --root "$data/user-docs"
run "存在しないルートは失敗する" 2 "" -- --root "$data/does-not-exist"

exit "$fail"
