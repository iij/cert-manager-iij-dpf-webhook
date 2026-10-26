# Specification Quality Checklist: DPF DNS01 Challenge Solver

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-09-18
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

全 16 項目パス。FR 79 件、SC 21 件。`/speckit-plan` へ進める状態。

**解消した検討項目**

- 書き換え対象の範囲限定 → solver 設定への `domainFilters` 追加として解消
  (「対象ゾーンの制限 (domainFilters)」節)。
- 複数クラスタが同一 ACME アカウントで同一チャレンジコードを共有する事象 →
  メタデータ用 TXT レコードによる解消 (「同一チャレンジコードの共有」節)。
  検討の過程で不採用とした案とその理由も同節に記録した。

**`/speckit-plan` で確定する値**

仕様は「何が閉じているか」を定め、閉じた集合の中身は plan に委ねている。
以下は値が未定だが、いずれも fail-closed の要件で囲んであるため、仕様としては確定している。

| 項目 | 仕様側の要件 |
|---|---|
| メタデータの件数上限 | 上限を定めること / 到達時は失敗させ追い出さないこと / 根拠を DNS プロトコル側に置くこと |
| 記録 1 件の表現 | 必須要素 3 つ / RDATA の 1 文字列上限に収まること |
| メタデータ用レコードの接頭辞 | 管理対象の先頭ラベルに付すこと / 長さ超過時は失敗させること |
| チャレンジの一意識別子 | cert-manager の要求に含まれる値を用いること / ロックオーナー単独で識別しないこと |
| チャレンジコードの識別表現 | 衝突耐性を持つこと |
| 回収の猶予 | チャレンジの最大所要時間より長いこと |
| webhook / exposed のポート番号 | TLS のみ / クライアント証明書検証 / 両者を同一ポートにしないこと |
| 集約 API サーバに必要な RBAC | 用途 1 と用途 2 に閉じていること / ワイルドカード禁止 |

**plan での実装時に守るべき設計判断**

- `domainFilters` の受け付け形式を 3 つ (`*` / `*.<ドメイン>` / 完全一致) に限定した。
  一般 glob は `*example.jp` が `evilexample.jp` に一致し、憲章が禁じる接尾辞一致になる。
  `*` の解釈を広げないこと。
- ロックオーナーは配備単位の必須設定とした。既定値を推測しないこと。
- RDATA の読み出しから書き戻しまでをゾーンロックの内側に収めること。
  ロック外で読んだ値をもとに書き戻すと lost update が起きる。

**Content Quality の判断根拠**

- 利用者が明示した実装上の制約 (cert-manager の webhook 枠組み、`dpf-go` のゾーンロック) は
  Assumptions に記載し、要件本文には持ち込んでいない。
- `domainFilters` のパターン形式、Description の書式、長さ上限 (80 / 32)、
  メタデータ用レコードの構成は、利用者に見える契約であり実装詳細ではないため要件本文に含めた。

**憲章との整合 (解消済み)**

- ClusterIssuer 用の既定 namespace の文言は、**憲章 v3.0.1 で本仕様と一致するよう改められた。**
- 憲章の「domain filter またはゾーンの許可リストが未設定の場合、いかなるレコードも管理対象と
  しないこと」は、`domainFilters` として実体を得たため改訂不要。
