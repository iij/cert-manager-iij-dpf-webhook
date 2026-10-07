<!-- SPDX-License-Identifier: Apache-2.0 -->

# CLAUDE.md

本リポジトリで作業するための手引き。規範は [`.specify/memory/constitution.md`](.specify/memory/constitution.md)
(憲章) にあり、本ファイルはそれを繰り返さない。食い違う場合は憲章が優先する。

## 概要

cert-manager の DNS01 solver。IIJ DNSプラットフォームサービス (DPF) のゾーンに ACME の
チャレンジ用 TXT を置く。kube-apiserver の集約層から呼ばれる APIService として動作する
(groupName `acme.dns-platform.jp`、solverName `iij-dpf`)。

設計の経緯と判断は `specs/001-dpf-dns01-solver/` にある (spec.md / plan.md / research.md の
D-xx / tasks.md)。判断を変える場合は research.md に追記する。

## ディレクトリ構成

| パス | 内容 |
|---|---|
| `cmd/webhook/` | 起動処理のみ。テスト対象外 (CI の起動時検証で確かめる) |
| `internal/solver/` | `webhook.Solver` の実装。cert-manager の契約への薄い変換層。solver 設定の解釈とトークンの namespace の決定 |
| `internal/challenge/` | 何を書くかの判断 (計画・期限・記録)。DPF に依存しない |
| `internal/dpf/` | DPF との境界。`dpf-go` の型とエラーはここから外へ出さない。要求・応答の記録もここで行う |
| `internal/dpf/dpftest/` | 模擬 DPF (テスト用) |
| `internal/tokensource/` | Kubernetes の Secret からトークンを読む (`dpf-go/misc/k8s`) |
| `internal/dnsname/` / `internal/zonefilter/` | ドメイン名の表現と `domainFilters` の判定 |
| `internal/observability/` | ログ・メトリクス・トレース・OTLP・healthz |
| `internal/testutil/` | テスト用に solver を組み立てる補助 |
| `deploy/chart/` | Helm チャート |
| `test/contract/` | cert-manager の webhook 契約のテスト |
| `test/integration/` | 模擬 DPF に対する結合テストとチャートの検証 (helm が必要。無ければ手元ではスキップ) |
| `test/e2e/` | 実 DPF とクラスタに対する検証 (`-tags e2e`。並列に実行しない) |
| `scripts/` | CI の検査 (SPDX・ドメイン名の判定・ACME の接続先) |

## コーディング規約

- すべての Go ファイルの先頭に `// SPDX-License-Identifier: Apache-2.0` を置く (テストも)
- ドメイン名の判定・変換に `strings` を使わない。`internal/dnsname` を用いる。ドメイン名と
  無関係な文字列への使用は、同じ行に `// domain-string: <理由>` を書いて検査から外す
- `dpf-go` の型・エラーを `internal/dpf` の外へ漏らさない。失敗は `dpf.Error` (恒久的／一時的の
  分類つき) に変換する
- ログは `log/slog`。要求ごとのロガーは `observability.WithLogger` / `observability.Logger` で
  context に載せて運ぶ。トークンを記録しない
- メトリクスのラベル・トレースの属性に、ゾーン名・レコード名・レコード値を入れない
- `nolint` には対象の linter と理由を書く
- 呼称は「IIJ DNSプラットフォームサービス」「IIJ DNS Platform Service」「DPF」のいずれか
- コメントと文書は日本語で、既存の文体 (である調) に合わせる

## 進め方

- テストファースト。**テストと実装は別のコミットにし、テストのコミットを先に置く。**
  テストのコミットの時点で、期待どおり失敗することを確かめる (CONTRIBUTING.md)
- 利用者に見える挙動を変えたら、同じ変更で `CHANGELOG.md` の `Unreleased` を更新し、
  README と食い違わないようにする
- 既定値は拒否側に置く。RBAC と NetworkPolicy を広げる変更には理由を書く
- テストと CI で ACME の本番環境に接続しない (`test/e2e/acmeserver` を経由する)

## よく使うコマンド

```bash
make check          # 整形・SPDX・ドメイン名の判定・ACME の接続先・静的解析・テスト
make test           # go test ./...
make lint           # golangci-lint (版は Makefile で固定)
make vuln           # govulncheck
go test ./test/integration/ -run TestChart   # チャートの検証 (helm が必要)

# 実環境の検証 (検証用ゾーンのみ。並列に実行しない)
DPF_API_TOKEN=... DPF_TEST_ZONE=... go test -tags e2e -p 1 ./test/e2e/
```
