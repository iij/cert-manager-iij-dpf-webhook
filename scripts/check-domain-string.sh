#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# ドメイン名を扱うパッケージで、strings パッケージによる名前の判定・変換が
# 使われていないことを検査する (憲章「ドメイン名の取り扱い」)。
#
# 検査は機械的な近似であり、レビューの代替ではない。ドメイン名と無関係な文字列に
# 対する正当な使用は、同じ行に `domain-string: <理由>` の注記を置いて除外する。
set -euo pipefail

root="${1:-.}"
pattern='strings\.(HasSuffix|TrimSuffix|HasPrefix|TrimPrefix|EqualFold|ToLower|ToUpper|Split|SplitN|SplitAfter|Contains|Index|LastIndex|Fields|TrimRight|TrimLeft|Trim)\('
dirs=(internal/dnsname internal/zonefilter internal/dpf internal/challenge internal/solver)

found=0
for d in "${dirs[@]}"; do
	[[ -d "$root/$d" ]] || continue
	while IFS= read -r line; do
		[[ -z "$line" ]] && continue
		if [[ "$line" == *'domain-string:'* ]]; then
			continue
		fi
		echo "ドメイン名の判定に strings を使っている可能性がある: $line" >&2
		found=1
	done < <(grep -rnE --include='*.go' --exclude='*_test.go' "$pattern" "$root/$d" || true)
done

exit "$found"
