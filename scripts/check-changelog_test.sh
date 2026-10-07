#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# scripts/check-changelog.sh のテスト (憲章「開発ワークフローと品質ゲート」の変更の記録)。
# 一時的な git リポジトリに変更を作り、基点との差分に対する判定を確かめる。
set -uo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
check="$here/check-changelog.sh"
fail=0

# repo <変更するファイル...>: 基点のコミットの上に、指定したファイルを変更したコミットを作る。
repo() {
	local dir
	dir="$(mktemp -d)"
	git -C "$dir" init -q
	git -C "$dir" config user.email test@example.invalid
	git -C "$dir" config user.name test
	mkdir -p "$dir/internal/solver" "$dir/deploy/chart/templates" "$dir/specs/001/contracts" "$dir/internal/dpf"
	for f in CHANGELOG.md internal/solver/config.go deploy/chart/values.yaml \
		deploy/chart/templates/rbac.yaml specs/001/contracts/solver-config.schema.json internal/dpf/apply.go; do
		echo base >"$dir/$f"
	done
	git -C "$dir" add -A && git -C "$dir" commit -qm base
	git -C "$dir" branch -q base
	for f in "$@"; do
		echo change >>"$dir/$f"
	done
	git -C "$dir" add -A && git -C "$dir" commit -qm change --allow-empty
	echo "$dir"
}

# run <名前> <期待する終了コード> <出力に含まれるべき文字列 (空なら検査しない)> -- <変更するファイル...>
run() {
	local name="$1" want="$2" needle="$3"
	shift 4
	local dir out code
	dir="$(repo "$@")"
	out="$(cd "$dir" && "$check" base 2>&1)"
	code=$?
	rm -rf "$dir"
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

run "solver 設定の変更に CHANGELOG が無ければ失敗する" 1 "internal/solver/config.go" -- internal/solver/config.go
run "チャートの変更に CHANGELOG が無ければ失敗する" 1 "deploy/chart/values.yaml" -- deploy/chart/values.yaml
run "チャートのテンプレートも対象にする" 1 "deploy/chart/templates/rbac.yaml" -- deploy/chart/templates/rbac.yaml
run "solver 設定のスキーマも対象にする" 1 "solver-config.schema.json" -- specs/001/contracts/solver-config.schema.json
run "CHANGELOG を伴えば成功する" 0 "" -- internal/solver/config.go deploy/chart/values.yaml CHANGELOG.md
run "対象外のファイルのみなら成功する" 0 "" -- internal/dpf/apply.go
run "変更が無ければ成功する" 0 "" --
run "判定が補助であることを示す" 1 "レビュー" -- internal/solver/config.go

exit "$fail"
