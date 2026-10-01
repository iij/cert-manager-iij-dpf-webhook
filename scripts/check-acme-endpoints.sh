#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# 本プロジェクトのテストと CI が、本番環境の ACME サーバを指していないことを検査する
# (憲章「ACME サーバの利用」/ research.md D-17)。
#
# 許可リストに無い ACME の接続先はすべて拒否する (原則 VI)。本番環境の一覧は、
# 拒否の理由を示すために持つ。対象は本プロジェクトのテストと CI の設定に限り、
# 利用者向けの文書 (README・specs 配下の導入例) は対象としない。
#
# 使い方: check-acme-endpoints.sh [--root DIR]
set -uo pipefail

root="."
while [[ $# -gt 0 ]]; do
	case "$1" in
	--root)
		root="$2"
		shift 2
		;;
	*)
		echo "不明な引数: $1" >&2
		exit 2
		;;
	esac
done

if [[ ! -d "$root" ]]; then
	echo "ルートが存在しない: $root" >&2
	exit 2
fi

# テストと CI が用いてよい接続先 (plan.md の Technical Context)。
allowed=(
	"https://acme-staging-v02.api.letsencrypt.org/directory"
)

# 本番環境の ACME サーバ (plan.md の Technical Context)。閉じた集合として管理する。
production=(
	"https://acme-v02.api.letsencrypt.org/directory"
	"https://dv.acme-v02.api.pki.goog/directory"
	"https://acme.zerossl.com/v2/DV90"
	"https://api.buypass.com/acme/directory"
)

# 検査の対象。本プロジェクトのテストと CI に限る。
targets=(test .github deploy/chart/ci)

contains() {
	local needle="$1"
	shift
	local x
	for x in "$@"; do
		[[ "$x" == "$needle" ]] && return 0
	done
	return 1
}

found=0
for t in "${targets[@]}"; do
	[[ -e "$root/$t" ]] || continue
	# URL を抽出し、ACME の接続先とみなせるもの (本番環境の一覧に一致するもの、
	# または acme / directory を含むもの) を判定の対象にする。
	while IFS=: read -r file line url; do
		[[ -z "$url" ]] && continue
		if contains "$url" "${allowed[@]}"; then
			continue
		fi
		if contains "$url" "${production[@]}"; then
			echo "本番環境の ACME サーバを指している: $file:$line: $url" >&2
			found=1
			continue
		fi
		lower="${url,,}"
		if [[ "$lower" == *acme* || "$lower" == */directory ]]; then
			echo "許可リストに無い ACME サーバを指している: $file:$line: $url" >&2
			found=1
		fi
	done < <(grep -rnoE 'https?://[A-Za-z0-9._~:/?#@!$&*+,;=%-]+' "$root/$t" 2>/dev/null || true)
done

exit "$found"
