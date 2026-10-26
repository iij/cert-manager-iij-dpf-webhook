#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# 利用者に見える契約に対応するファイルを変えた変更が、CHANGELOG.md の更新を伴うことを検査する
# (憲章「開発ワークフローと品質ゲート」の変更の記録)。
#
# 使い方: check-changelog.sh <基点のリビジョン>
#   基点から HEAD までの差分 (<基点>...HEAD) を対象にする。
#
# 「利用者に見えるか」は機械的に判定できない。本検査は補助であり、レビューの代替ではない。
# 対象外のファイルで利用者に見える挙動を変えた場合も、CHANGELOG.md を更新すること。
set -euo pipefail

if [[ $# -ne 1 ]]; then
	echo "usage: $0 <base-revision>" >&2
	exit 2
fi
base="$1"

# 利用者に見える契約に対応するパス (正規表現)。
contract='^(internal/solver/config\.go|deploy/chart/.*|specs/[^/]+/contracts/solver-config\.schema\.json)$'

changed="$(git diff --name-only "$base...HEAD")"
touched="$(grep -E "$contract" <<<"$changed" || true)"
if [[ -z "$touched" ]]; then
	exit 0
fi
if grep -qx 'CHANGELOG.md' <<<"$changed"; then
	exit 0
fi

echo "利用者に見える契約に対応するファイルが変更されたが、CHANGELOG.md が更新されていない:" >&2
sed 's/^/  /' <<<"$touched" >&2
echo "利用者に見える挙動を変えた場合は CHANGELOG.md の Unreleased に記録すること。" >&2
echo "本検査は補助であり、レビューの代替ではない。利用者に見える変更でないとレビューで" >&2
echo "判断した場合は、その旨を Pull Request に記載したうえで本検査を省略してよい。" >&2
exit 1
