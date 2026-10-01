#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# すべての Go ファイル (テストを含む) の 1 行目が SPDX ヘッダであることを検査する
# (憲章「ライセンス」)。欠けたファイルを列挙して失敗する。
set -euo pipefail

root="${1:-.}"
want='// SPDX-License-Identifier: Apache-2.0'
missing=0

while IFS= read -r -d '' f; do
	if [[ "$(head -n 1 "$f")" != "$want" ]]; then
		echo "SPDX ヘッダがない: $f" >&2
		missing=1
	fi
done < <(find "$root" -name '*.go' -not -path '*/vendor/*' -not -path '*/.git/*' -print0)

exit "$missing"
