---
description: "Task list for DPF DNS01 Challenge Solver"
---

# Tasks: DPF DNS01 Challenge Solver

**Input**: Design documents from `/specs/001-dpf-dns01-solver/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md),
[data-model.md](./data-model.md), [contracts/](./contracts/)

**Tests**: **必須。** 憲章 原則 III (テストファースト) は NON-NEGOTIABLE であり、
「実装より前にテストを書き、期待どおり失敗することを確認してから実装に着手すること (MUST)」
を求める。**各実装タスクは、それを検証するテストタスクの後に置く。** テストタスクの説明に
対応する実装タスクを「→ Txxx」で示す。

**テストを持たない成果物**: `cmd/webhook/main.go` (起動処理のみ。plan.md の Structure Decision
によりテスト対象から外し、CI の起動時検証 T119 で確かめる)、`Dockerfile` とリリース設定
(CI の検証 T122 / T125 で確かめる)、文書類。

**ロックの検証の範囲**: ゾーンロックの挙動 (取得・読み出し・反映・解放の順序、期限内のロックを
奪わないこと、奪われた場合に書き込まないこと、待機、同時取得の排他、エラー時の解放、一時レコード
の後始末) は `github.com/iij/dpf-go` の `utils.Mutex` / `utils.ZoneApplier` が実装し検証する。
**本リポジトリでは再検証しない** (FR-044〜046, FR-051〜054, FR-063。並行処理の成功基準 SC-007 / SC-011 も同様)。本リポジトリが検証するのは
ライブラリの使い方 (構成、`ErrSkipApply`、操作期限、エラーの扱い) に限る。DPF API は HTTPS でのみ
通信できるため、TLS 検証の有無も検証の対象としない。

**変更の記録**: 憲章 v3.1.0 の「変更の記録」により、**利用者に見える挙動を変更した場合は
同じ変更のなかで `CHANGELOG.md` を更新しなければならない (MUST)。** 各ユーザーストーリーの
末尾に記録のタスクを置く。README と食い違う場合は README も同じ変更で直す。

**Organization**: ユーザーストーリー単位に分け、各ストーリーを独立に実装・検証できるようにする。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 並行実行できる (別ファイル、未完了タスクへの依存なし)
- **[Story]**: 対応するユーザーストーリー (US1 / US2 / US3)

## Path Conventions

単一 Go モジュール。`cmd/` `internal/` `deploy/` `test/` をリポジトリルートに置く
(plan.md の Project Structure を参照)。

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: プロジェクトの初期化と、憲章が求める品質ゲートの土台

- [X] T001 `go.mod` を作成し go ディレクティブを `1.27` とする。依存を `github.com/cert-manager/cert-manager v1.21.2` / `github.com/iij/dpf-go v0.6.0` / `github.com/iij/dpf-go/misc/k8s` / `github.com/miekg/dns` / `k8s.io/client-go` に固定する
- [X] T002 [P] `LICENSE` に Apache License 2.0 を置き、著作権表示を `Copyright 2026 Internet Initiative Japan Inc.` とする
- [X] T003 [P] `CHANGELOG.md` を Keep a Changelog 形式で作成する。`Unreleased` の節と、SemVer に従う旨の注記を置く。憲章 v3.1.0 が求める「変更の記録」の受け皿とする
- [X] T004 [P] `.golangci.yml` を作成し、有効な linter とバージョンを固定する。ローカルと CI が同一設定で動くようにする
- [X] T005 [P] `scripts/check-spdx.sh` を作成し、全 `.go` ファイル先頭の `// SPDX-License-Identifier: Apache-2.0` を検査する
- [X] T006 [P] `scripts/check-domain-string.sh` を作成し、ドメイン名の判定に `strings` パッケージが使われていないことを検査する (憲章のレビュー観点の自動化)
- [X] T007 [P] `scripts/testdata/acme-endpoints/` に検査用の入力を置き、`scripts/check-acme-endpoints_test.sh` に `scripts/check-acme-endpoints.sh` のテストを書く。**許可リスト (Let's Encrypt のステージング環境) のみを含む入力で成功すること**、本番環境の一覧 (plan.md の Technical Context) のいずれかを含む入力で失敗し拒否の理由に該当の接続先を示すこと、**一覧に無い未知の ACME サーバを含む入力でも失敗すること**、利用者向けの文書 (`README.md`・`specs/**/quickstart.md`) を検査の対象にしないことを含める (憲章「ACME サーバの利用」/ research.md D-17) → T008
- [X] T008 `scripts/check-acme-endpoints.sh` を作成する。`test/`・`.github/`・CI が用いる values に現れる ACME のディレクトリ URL を抽出し、許可リスト外を拒否する。本番環境の一覧に該当する場合はその旨を示す。対象は本プロジェクトのテストと CI に限る (plan.md の Technical Context)
- [X] T009 [P] `Makefile` に `fmt` / `lint` / `vuln` / `test` / `build` / `spdx` / `acme-endpoints` の各ターゲットを定義する
- [X] T010 [P] `.github/pull_request_template.md` を作成し、**テストが実装に先行したことを追跡できる欄**を設ける。テストを追加したコミット、そのテストが失敗したことの確認 (実行結果)、実装のコミットを記入させる。バグ修正では再現テストのコミットを記入させる。あわせてコミットの規約 (テストと実装を別コミットにし、テストを先に置く) を `CONTRIBUTING.md` に記載する (憲章 原則 III)
- [X] T011 plan.md の Project Structure に従って `cmd/webhook/` `internal/{solver,challenge,zonefilter,dnsname,dpf,tokensource,observability,version}/` `deploy/chart/` `test/{contract,integration,e2e}/` のディレクトリを作成する

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: すべてのユーザーストーリーが依存する中核。ここが揃うまでストーリーに着手しない

**⚠️ CRITICAL**: T012〜T015 は**設計の前提を実物で確かめるタスク**であり、結果によっては
経路の見直しが必要になる。**最初に実行する** (research.md §4)

### 設計前提の検証 (Spike)

- [ ] T012 `test/e2e/premise_timing_test.go` を作成し、検証用ゾーンに対して排他の取得の各段 (一時レコードの作成・ゾーンのラベルの書き込み・一時レコードの取り消し)・反映・解放の所要時間を個別に計測する。**60 秒に収まることは求めない。**計測は操作期限の既定 (5 分) の妥当性の根拠とする。あわせて**ジョブ待ちを除いた本サービス自身のオーバーヘッド** (SC-003)、ゾーンのレコード数と所要時間の関係、**アクセストークンでゾーンのラベルを読み書きできること**、**別トークンの 2 プロセス間で同時取得が起こらないこと**を記録する (research.md §4 1 / 1b / 5a)
- [ ] T013 `test/e2e/premise_convergence_test.go` を作成し、**1 回の処理が 60 秒 (`k8s.io/apiserver@v0.36.2/pkg/server/config.go:453` の `RequestTimeout` 既定) を越える場合に、呼び出し元がタイムアウトしたのちも処理が完了し、再試行が変更 API を呼ばずに成功すること**を確認する。先の処理が走行中に来た再試行が排他を待ち、`ErrSkipApply` で反映を省くことも含める (research.md D-15 / SC-027)
- [ ] T014 `test/e2e/premise_deadline_test.go` を作成し、操作期限に達した場合に**操作期限を越えて DPF への書き込みが行われないこと**と、ロックが TTL の満了で失効することを確認する。`RunLocked` の解放が期限切れの context を用いるため解放は失敗する (research.md D-15 / SC-004 / SC-013)
- [ ] T015 `test/e2e/premise_nocollateral_test.go` を作成し、TTL が null のもの・ラベル付き・コメント付きを含む無関係なレコードを検証用ゾーンに置いたうえで `ZoneApplier.Apply` を実行し、**RDATA・TTL・ラベル・コメントのいずれも変わらないこと**と、SOA および Zone Apex NS が置き換わらないことを確認する (FR-055 / FR-056)

**Checkpoint A**: T013/T015 が通らない場合、research.md の D-13 / D-15 を見直してから先へ進む

### ドメイン名と範囲判定

- [X] T016 [P] `internal/dnsname/name_test.go` に、正規化名の生成・比較・境界での一度だけの正規化、`dns.IsDomainName` による妥当性検証を検証するテストを書く → T018
- [X] T017 [P] `internal/zonefilter/filter_test.go` に、`domainFilters` のパースと判定のテストを書く。**空/未指定で何も許可しないこと**、`*` / `*.<ドメイン>` / 完全一致の 3 形式のみ受け付けること、`*example.jp` などラベル境界に揃わないパターンを**拒否**すること、`*.example.jp` が `example.jp` 自身に**一致しない**こと、大文字小文字と末尾ドットで結果が変わらないことを含める (FR-016, FR-019〜025) → T019
- [X] T018 `internal/dnsname/name.go` に正規化名の型を実装する。`dns.CanonicalName` を用い、`strings` による自前実装を行わない
- [X] T019 `internal/zonefilter/filter.go` に `domainFilters` のパースと判定を実装する。判定は `dns.IsSubDomain` と正規化名の一致で行い、`strings` を用いない

### チャレンジのドメインロジック (DPF に依存しない)

- [X] T020 [P] `internal/challenge/digest_test.go` に、対象の識別値の算出テストを書く。`SHA-256(チャレンジコード)` の先頭 64 ビットを 16 進 16 文字で表すことを固定する。**保持者の識別値は持たない** (D-02 / D-03 / FR-070 / FR-071) → T025
- [X] T021 [P] `internal/challenge/expire_test.go` に、期限の記録 (ラベル) の符号化・復号・回収判定のテストを書く。キーが `expire.acme.dns-platform.jp/<対象>`、値が Unixtime であること、**prefix が一致しないラベルを維持すること**、**対応するラベルを持たない RDATA を回収しないこと**、期限切れのラベルを RDATA の有無によらず取り除くこと、上限 10 到達で失敗させ**追い出しをしないこと**を含める (FR-066〜069, FR-072〜076, FR-086) → T026
- [X] T022 [P] `internal/challenge/plan_test.go` に、現在の状態と要求から `ChangeSet` を導くテストを書く。data-model.md の状態遷移表を網羅する。**`CleanUp` が当該コードの RDATA とラベルを削除しないこと**、**回収すべきものが無い `CleanUp` が「変更なし」を返すこと**、`Present` が RDATA を重複させず期限を閾値未満のときのみ更新すること、**他者の RDATA とラベルを維持すること**、既存レコードへの追記で TTL を変更しないこと、回収で RDATA が空になる場合にレコードごと削除する計画を返すこと、TXT 以外を対象にしないことを含める (FR-026〜039, FR-066, FR-077〜083) → T027
- [X] T023 [P] `internal/challenge/plan_test.go` に、**呼び出し元がタイムアウトしたのち処理が成功した場合の再試行**のテストを書く。`Present` の再試行で RDATA が重複せず、閾値内なら変更なしを返すこと、`CleanUp` の再試行が何も起こさないことを含める。**期限による回収では「ずれる量」が存在しない**ことを固定する (research.md D-15 / SC-005) → T027
- [X] T024 [P] `internal/challenge/plan_test.go` に、**再試行ストームへの配慮**のテストを書く。閾値の期間内に同一の提示を 10 回繰り返したとき、変更が生じるのが 1 回目のみであることを確認する。**RDATA はあるがラベルが無い場合は変更が生じること**も含める (FR-078 / FR-079 / SC-026) → T027
- [X] T025 `internal/challenge/digest.go` を実装する
- [X] T026 `internal/challenge/expire.go` を実装する
- [X] T027 `internal/challenge/plan.go` を実装する。**この層は DPF の型を参照しない**

### DPF クライアント層 (憲章 原則 II の境界)

- [X] T028 [P] `internal/dpf/errors_test.go` に、DPF と `misc/k8s` のエラーを本プロジェクトのエラー型へ変換するテストを書く。`ErrSecretNotFound` / `ErrKeyNotFound` / `ErrEmptySecret` / `ErrForbidden` を**恒久的な失敗**、到達失敗を**一時的な失敗**に分類すること、`ErrStillLock` / `ErrNotLockHolder` / `ErrLabelLimit` (v0.6.0 では**ゾーンの**ラベルの枠が足りない) / `ErrOwnerTooLong` を判別できることを含める。`ErrLabelLimit` は恒久的な失敗に分類する。**`dpf-go` の型が戻り値に現れないこと**を確かめる → T035
- [X] T029 [P] `internal/dpf/ratelimit_test.go` に、共通 RoundTripper のテストを書く。**複数のクライアントから送った要求が 1 つのレート制限 (5 req/s, burst 10) と同時実行数 (5) を共有すること**を確認する (D-09) → T036
- [X] T030 [P] `internal/dpf/client_test.go` に、クライアントのキャッシュのテストを書く。トークンの出所 (namespace, Secret 名, key) が同じなら同じクライアントを返し、異なれば別のクライアントを返すこと、いずれも共通の RoundTripper を用いること、**トークンが要求ごとに評価されること** (ローテーション追随) を含める (D-09 / FR-011) → T037
- [X] T031 [P] `internal/dpf/zone_test.go` に、ゾーン特定のテストを書く。**`ResolvedZone` と一致するゾーンを選ぶこと**、**チャレンジ対象の FQDN と longest match するゾーンが `ResolvedZone` の子ゾーンである場合に、それを選ばないこと**、**`ResolvedZone` が無く親ゾーンだけがある場合に親ゾーンへ読み替えず失敗すること** (`utils.GetZoneFromName` が親ゾーンを返す場合を差し替えで再現する)、`ResolvedFQDN` が `ResolvedZone` の配下に無ければ失敗すること、比較が正規化名で行われること、見つからない場合に `ResolvedZone` と対象 FQDN を含むエラーを返すことを含める (FR-013〜015 / research.md D-18) → T038
- [X] T032 [P] `internal/dpf/record_test.go` に、**排他を取らない公開レコードの読み出し**のテストを書く。対象名の TXT の RDATA とラベルを返すこと、ラベル・TTL の null を失わないことを含める (D-11 の 2a / FR-084) → T039
- [X] T033 [P] `internal/dpf/apply_test.go` に、`ZoneApplier` のラップのテストを書く。編集関数が対象以外のレコードをそのまま返すこと、**結果が入力と変わらない場合に `utils.ErrSkipApply` を返し、`PatchZoneAtomicChanges` が呼ばれないこと** (FR-084a)、context が打ち切られている場合は `ErrSkipApply` ではなく `ctx.Err()` を返すこと、**反映の後に解放だけが失敗した場合も失敗として返すこと** (FR-059)、書き込み直前に `domainFilters` を再判定すること (FR-018)、**構成に `utils.WithLocker` を含まないこと** (D-16)、ロックオーナー・TTL 2 分・待機間隔 5 秒で構成されること、**反映の Description が `by cert-manager(<ロックオーナー>)` であり、ロックオーナーが 32 文字のとき 49 文字で上限 80 に収まり切り詰めが起きないこと** (FR-064 / SC-014) を含める。`dpf.RecordsApi` / `ZonesApi` (v0.6.0 の `GetZoneLabels` / `PutZoneLabels` を含む) / `JobsApi` を差し替えて実 API に到達しない (FR-042, FR-043, FR-059〜062, FR-084a) → T040, T041, T068
- [X] T034 [P] `internal/tokensource/provider_test.go` に、`misc/k8s` の `NewTokenProvider` の組み立てのテストを書く。**namespace・Secret 名・key を明示的に渡すこと**、自前で Secret を読まないこと、トークンの値がエラーに含まれないことを含める (D-09 / FR-006 / FR-012) → T042
- [X] T035 `internal/dpf/errors.go` を実装する。`dpf-go` の型とエラーを上位へ漏らさない
- [X] T036 `internal/dpf/ratelimit.go` に、プロセス全体で 1 つの RoundTripper を実装する。レート制限と同時実行数がクライアントのキャッシュごとに分かれないようにする (D-09)
- [X] T037 `internal/dpf/client.go` に、トークンの出所 (namespace, Secret 名, key) をキーとするクライアントのキャッシュを実装する。共通の RoundTripper を `WithHTTPClient` で注入する
- [X] T038 [P] `internal/dpf/zone.go` に、`ResolvedZone` と一致するゾーンの特定を実装する。`utils.GetZoneFromName(ctx, cz, ResolvedZone, false)` の結果の名前が `ResolvedZone` と正規化名で一致することを確かめ、一致しなければ失敗させる (research.md D-18)
- [X] T039 [P] `internal/dpf/record.go` に、排他を取らない公開レコードの読み出しを実装する (D-11 の 2a)
- [X] T040 `internal/dpf/apply.go` に `utils.ZoneApplier` のラップを実装する。**`PatchZoneAtomicChanges` を直接呼ばない**。`NewZoneApplier(..., WithLockOptions(WithOwner(owner), WithTTL(2*time.Minute)), WithHoldOptions(WithLockWait(5*time.Second)))` の形で構成し、`Apply` に `WithApplyDescription` を渡す。編集関数は変更が不要なら `utils.ErrSkipApply` を返す (D-11 / D-13)
- [X] T041 `internal/dpf/apply.go` で **`utils.WithLocker` を呼ばない**ことを担保する。既定の排他は 2 者の同時取得をユーザをまたいで防ぐが、差し替えるとその範囲がその仕組みの利用者に狭まる (D-16)
- [X] T042 `internal/tokensource/provider.go` に、`misc/k8s` の `NewTokenProvider` を要求ごとに組み立てる処理を実装する。namespace は必ず明示的に渡す。自前で Secret を読まない (D-09)

### solver の受け口と設定

- [X] T043 [P] `internal/solver/config_test.go` に、solver 設定のパースのテストを書く。既定値 (`endpoint` / `dpf-token` / `token` / `domainFilters` が空)、**未知フィールドの拒否**、`https` 以外の `endpoint` の拒否、既定値へフォールバックせず失敗させること、**`domainFilters` に `*` が含まれる場合に警告が記録されること** (FR-023a) を含める (FR-003〜005) → T046
- [X] T044 [P] `internal/solver/options_test.go` に、配備単位の設定の検証テストを書く。**ロックオーナー未設定で起動を中止すること**、33 文字以上と英数字・`.`・`-`・`_` 以外を含む値を拒否すること、猶予・操作期限・ロック TTL が非正なら中止すること、閾値が猶予より短いことを含める (FR-047〜050, FR-080) → T047
- [X] T045 [P] `internal/solver/deadline_test.go` に、操作期限のテストを書く。**既定が 60 秒より長い (5 分) こと**、**操作期限の context が要求の context から派生していないこと** (呼び出し元が打ち切っても処理が続くこと)、**操作期限が DPF への呼び出しまで伝播し、操作期限を越えて書き込みが行われないこと**、期限切れの context ではロックの解放が失敗しうること、その場合に処理の正しさが解放の成否に依存しないことを含める (FR-057, FR-057a, FR-058, FR-059) → T067
- [X] T046 `internal/solver/config.go` を実装する。`*` を含む `domainFilters` を受け付けた場合は警告を記録する (FR-023a)
- [X] T047 `internal/solver/options.go` を実装する
- [X] T048 [P] `internal/version/version_test.go` に、ビルド時に埋め込んだ値 (バージョン・リビジョン) が読み出せ、未設定時に既定の表記になることのテストを書く → T049
- [X] T049 `internal/version/version.go` にビルド情報を埋め込む変数を定義する

### 可観測性の土台

- [X] T050 [P] `internal/observability/log_test.go` に、構造化ログのテストを書く。**標準出力への出力が常に有効で、他の出力先の設定で停止しないこと**、ログレベルを設定で変えられることを含める (憲章 原則 V) → T052
- [X] T051 [P] `internal/observability/exposed_test.go` に、exposed サーバのテストを書く。8080 で `/healthz` `/readyz` `/metrics` のみを返し、それ以外のパスに応答しないこと、**webhook ポートと同一にならないこと**を含める (D-07 / FR-093) → T053
- [X] T052 `internal/observability/log.go` に構造化ログを実装する
- [X] T053 `internal/observability/exposed.go` に、8080 で `/healthz` `/readyz` `/metrics` のみを提供する HTTP サーバを実装する
- [X] T054 `cmd/webhook/main.go` に `cmd.RunWebhookServer("acme.dns-platform.jp", solver)` の呼び出しのみを書く。テスト対象にしないため最小に保つ (起動時の挙動は T119 で検証)

**Checkpoint B**: 土台が揃い、ユーザーストーリーの実装に着手できる

---

## Phase 3: User Story 1 - ClusterIssuer でクラスタ全体の証明書を取得する (Priority: P1) 🎯 MVP

**Goal**: 管理者がアクセストークンの Secret と ClusterIssuer を用意するだけで、
DPF 管理下のドメインの証明書を取得できる

**Independent Test**: ClusterIssuer と `dpf-token` Secret を用意し、DPF 管理下のドメインに
対する Certificate を作成する。証明書が Ready になれば成立する

### Tests for User Story 1 ⚠️ 先に書き、失敗を確認する

- [X] T055 [P] [US1] `test/contract/solver_test.go` に `webhook.Solver` の契約テストを書く。`Name()` が `iij-dpf` を返すこと、`Present` / `CleanUp` が冪等であること、`Initialize` が配備単位の設定を検証して不足時にエラーを返すことを含める (FR-001 / FR-037 / FR-050) → T063
- [X] T056 [P] [US1] `test/integration/clusterissuer_test.go` に、`AllowAmbientCredentials` が `true` の要求でトークン取得元 namespace が決まることのテストを書く。`tokenSecretRef.namespace` の指定あり / 省略時は**本 Pod の namespace**の 2 経路を含める (FR-006) → T063
- [X] T057 [P] [US1] `test/integration/wildcard_test.go` に、ワイルドカード名の Certificate で `_acme-challenge.example.com` に対する TXT が作られることのテストを書く → T065
- [X] T058 [P] [US1] `test/integration/cname_test.go` に、CNAME 委譲された要求で**委譲先の `ResolvedZone` と一致するゾーン**に対して `domainFilters` が判定されることのテストを書く。委譲元が許可されていても委譲先が不許可なら失敗すること、書き込み対象の名前を `_acme-challenge` に限定しないこと (FR-013 / FR-017 / FR-040 / FR-041) → T065
- [X] T059 [P] [US1] `test/integration/retry_after_timeout_test.go` に、**呼び出し元のタイムアウト後に処理が成功し、その後に再試行が来る**場面のテストを書く。`Present` / `CleanUp` の双方で記録の数と RDATA の数がずれないこと、**先の処理が走行中の再試行が排他を待ち、反映を省くこと**を確認する (SC-005 / SC-027) → T065, T066, T067
- [X] T060 [P] [US1] `test/integration/no_change_test.go` に、**望む最終状態が現状と一致する場合に変更 API が 0 回であること**のテストを書く。回収対象の無い `CleanUp` と、閾値内の `Present` の再試行の双方で、排他の取得も反映も行わず成功を返すこと (FR-083〜085 / SC-025) → T064
- [X] T061 [P] [US1] `test/integration/cleanup_test.go` に、**`CleanUp` が当該チャレンジの RDATA と記録を削除せず、期限切れの回収のみを行うこと**のテストを書く。回収で RDATA が空になる場合はレコードごと削除されること、Present を伴わない `CleanUp` が成功すること (FR-033〜035, FR-038, FR-082) → T066
- [X] T062 [P] [US1] `test/integration/chart_us1_test.go` に、チャートが生成する APIService・Deployment・RBAC・values の検証を書く。APIService が `acme.dns-platform.jp/v1alpha1` とポート 443 であること、Deployment のセキュリティ設定 (非 root の固定数値 UID、`runAsNonRoot` / `allowPrivilegeEscalation: false` / `readOnlyRootFilesystem: true` / `capabilities.drop: ["ALL"]` / `seccompProfile.type: RuntimeDefault`)、**RBAC が Secret に `get` のみでワイルドカードを含まないこと**、values の既定が拒否側でトークンの Secret を作らないこと (D-08) → T069〜T072

### Implementation for User Story 1

- [X] T063 [US1] `internal/solver/solver.go` に `Name()` と `Initialize()` を実装する。`Initialize` が受け取る `*rest.Config` から `misc/k8s` の SecretsAPI を作り、配備単位の設定を検証する
- [X] T064 [US1] `internal/solver/solver.go` に、**望む最終状態が現状と一致する場合にゾーンへの変更を行わない**分岐を実装する。要否の判定は排他を取らずに `internal/dpf/record.go` で公開レコードを読んで行う (research.md D-06 / D-11 / FR-083〜085)
- [X] T065 [US1] `internal/solver/solver.go` に `Present()` を実装する。ゾーン特定 → `domainFilters` 判定 → 事前判定 → `ZoneApplier.Apply` の順に呼ぶ (D-11)
- [X] T066 [US1] `internal/solver/solver.go` に `CleanUp()` を実装する。**期限切れの回収のみを行い、当該チャレンジの RDATA と記録を削除しない。**回収すべきものが無ければ何も変更しない (FR-072〜076 / FR-082)
- [X] T067 [US1] `internal/solver/solver.go` に操作期限を実装する。`Present` / `CleanUp` に context が渡されないため自前で持つ。**既定 5 分とし、要求の context から派生させない。**操作期限に達したときはロックの解放を試みてエラーを返す (FR-057〜059)
- [X] T068 [US1] `internal/dpf/apply.go` の `Description` に `by cert-manager(<ロックオーナー>)` を設定する。形式以外を付与せず、切り詰めない (FR-064)
- [X] T069 [P] [US1] `deploy/chart/templates/apiservice.yaml` に APIService を定義する。`acme.dns-platform.jp/v1alpha1`、Service ポート 443
- [X] T070 [P] [US1] `deploy/chart/templates/deployment.yaml` に Deployment を定義する。T062 のセキュリティ設定を満たす
- [X] T071 [P] [US1] `deploy/chart/templates/rbac.yaml` に RBAC を定義する。**Secret は `get` のみ**、`kube-system` の `extension-apiserver-authentication` に `get`/`list`/`watch`、`tokenreviews`/`subjectaccessreviews` に `create`。ワイルドカードを用いない (D-08)
- [X] T072 [P] [US1] `deploy/chart/values.yaml` に既定値を定義する。**既定は拒否側**。トークンの Secret をチャートが作らない
- [X] T073 [P] [US1] `test/e2e/acmeserver/acmeserver_test.go` に、e2e が用いる ACME サーバの解決のテストを書く (e2e のビルドタグを付けず通常の `go test` で走る)。**`ACME_SERVER` が未設定なら Let's Encrypt のステージング環境を返すこと**、許可リストの値をそのまま返すこと、**本番環境の一覧の値や未知の値ではエラーを返し e2e を開始しないこと** (憲章「ACME サーバの利用」/ research.md D-17) → T074
- [X] T074 [US1] `test/e2e/acmeserver/acmeserver.go` に、`ACME_SERVER` から e2e の ACME サーバを解決する処理を実装する。未設定時の既定はステージング環境とし、本番環境へ既定で接続しない
- [ ] T075 [US1] `test/e2e/us1_clusterissuer_test.go` に、実 DPF と検証用ゾーンに対する通し検証を書く。**ClusterIssuer の ACME サーバは `test/e2e/acmeserver` で解決した値 (既定は Let's Encrypt のステージング環境) とし、本番環境を用いない。**提示 → 反映で証明書が発行されること、**後片付けの時点では RDATA とラベルが残り、期限を過ぎてから同一の名前を操作すると回収されること** (SC-024)、変更履歴の `Description` から配備を判別できること (SC-012) を確認する。あわせて、**証明書の更新を 10 回繰り返したのちに残るチャレンジ用 RDATA が猶予の期間に生じたもののみであること** (SC-006) と、**本サービス以外の RDATA を持つ TXT レコードに対して提示と後片付けを 10 回繰り返しても当初の RDATA と TTL が元のまま残ること** (SC-020) を確認する
- [ ] T076 [US1] `test/e2e/us1_multicluster_test.go` に、**異なるロックオーナーを持つ 2 つの配備**から (ACME サーバは `test/e2e/acmeserver` で解決した値。本番環境を用いない)、同一の ACME アカウントで同一の名前の証明書を同時に取得する検証を書く。両方の証明書が取得できること、チャレンジレコードのラベルが 1 件であること、一方の後片付けで他方の RDATA が消えないこと (SC-021 / quickstart.md §8)。配備は同一クラスタ内の別 namespace で模してよい。**(実装時の注記)** APIService は 1 つの API グループにつき 1 つしか登録できず同一クラスタに 2 つの配備を置けないため、ロックオーナーの異なる 2 つの solver で模す
- [X] T077 [US1] `CHANGELOG.md` の `Unreleased` に US1 が加えた**利用者に見える挙動**を記録する。solver 設定のスキーマ (`endpoint` / `tokenSecretRef` / `domainFilters`)、groupName と solverName、チャートが生成するリソースと RBAC、ゾーン反映の Description の形式、**使用済みの RDATA が期限まで残ること**、**同じゾーンを触る他のプログラムも `dpf-go` v0.6.0 以降にそろえる必要があること**が対象 (憲章「変更の記録」)
- [X] T078 [US1] `README.md` に US1 時点の導入手順を記載し、`CHANGELOG.md` の記載と食い違いがないことを確認する

**Checkpoint C**: US1 が単独で成立する。ここまでで MVP

---

## Phase 4: User Story 2 - namespace に閉じた Issuer でテナントごとに証明書を取得する (Priority: P2)

**Goal**: 各チームが自分の namespace のトークンで証明書を取得でき、他チームの認証情報に
到達できない

**Independent Test**: 2 つの namespace に別々のトークンと Issuer を置き、各 namespace の
Certificate がそれぞれのトークンで処理されること、および一方から他方の Secret に
到達できないことを確認する

### Tests for User Story 2 ⚠️ 先に書き、失敗を確認する

- [X] T079 [P] [US2] `test/integration/namespaced_issuer_test.go` に、`AllowAmbientCredentials` が `false` の要求で `ResourceNamespace` の Secret のみを読むことのテストを書く (FR-006) → T084
- [X] T080 [P] [US2] `test/integration/namespace_isolation_test.go` に、`tokenSecretRef.namespace` が `ResourceNamespace` 以外を指す場合に**要求を失敗させ、読み替えないこと**のテストを書く (FR-007 / SC-008) → T084
- [X] T081 [P] [US2] `test/integration/token_failure_test.go` に、Secret 不在・key 不在・値が空の場合を**恒久的な失敗**として再試行しないこと、到達失敗を**一時的な失敗**として上限付きで再試行すること、分類をログに記録することのテストを書く (FR-008〜010) → T085
- [X] T082 [P] [US2] `test/integration/token_rotation_test.go` に、外部でトークンがローテーションされた場合に再起動なしで新しい値が使われることのテストを書く (FR-011) → T085
- [X] T083 [P] [US2] `test/integration/chart_rbac_namespaced_test.go` に、対象 namespace を列挙して Role/RoleBinding のみで構成できること、ClusterIssuer を使わない構成で ClusterIssuer 用の既定 namespace に権限を付与しないことの検証を書く (D-08) → T086

### Implementation for User Story 2

- [X] T084 [US2] `internal/solver/config.go` に `AllowAmbientCredentials` による取得元 namespace の決定を実装する。`false` で他 namespace が指定された場合は失敗させ、`ResourceNamespace` へ読み替えない (FR-006〜007)
- [X] T085 [US2] `internal/tokensource/provider.go` に恒久的／一時的な失敗の分類を実装する。`misc/k8s` のエラー型で判別する (D-09 / FR-008〜010)
- [X] T086 [US2] `deploy/chart/templates/rbac.yaml` に、対象 namespace を列挙して Role/RoleBinding のみで構成できる形を追加する。ClusterRole による全 namespace への付与を唯一の選択肢としない (D-08)
- [ ] T087 [US2] `test/e2e/us2_namespaced_test.go` に、2 つの namespace を用いた実環境検証を書く。**(実装時の注記)** クラスタの検証は、`tokenSecrets.namespaces` に `E2E_US2_NAMESPACES` (既定 `e2e-team-a,e2e-team-b`) を与えてチャートを導入し、その namespace を作っておく必要がある (T124)
- [X] T088 [US2] `CHANGELOG.md` の `Unreleased` に US2 が加えた利用者に見える挙動を記録する。`AllowAmbientCredentials` による取得元 namespace の決定、`tokenSecretRef.namespace` の扱い、恒久的／一時的な失敗の分類、namespace を列挙した RBAC の構成が対象

**Checkpoint D**: US1 と US2 が独立に成立する

---

## Phase 5: User Story 3 - 失敗したチャレンジの原因を切り分ける (Priority: P3)

**Goal**: 証明書が発行されないとき、Pod のログだけで原因が設定・トークン・DPF のどれかを
判断できる

**Independent Test**: 誤ったトークン・存在しないゾーン・不正な config をそれぞれ与え、
ログから原因を判別できることを確認する

### Tests for User Story 3 ⚠️ 先に書き、失敗を確認する

- [X] T089 [P] [US3] `test/integration/error_logging_test.go` に、DPF のエラー応答が**全文**記録され、問い合わせ用の識別子が失われないことのテストを書く。長さで切り詰めないこと (FR-088) → T096
- [X] T090 [P] [US3] `test/integration/request_logging_test.go` に、**DPF および Kubernetes API へ送った要求の内容** (メソッド・パス・ゾーン ID・要求本文) が記録され、**一括置換の要求では本文の代わりに加除の差分 (レコード名・RDATA・ラベル) とレコードの総数が記録され、ゾーン全体のレコードが記録されないこと**、それ以外の要求の本文が切り詰められないこと、**アクセストークンと認証ヘッダが含まれないこと**、記録が webhook の HTTP 応答本文に現れないことのテストを書く (FR-088a / FR-088b) → T096
- [X] T091 [P] [US3] `test/integration/secret_redaction_test.go` に、トークンの値がログ・エラーメッセージ・メトリクス・トレース・HTTP 応答本文のいずれにも現れないことのテストを書く。**認証情報の非出力が全文記録に優先すること**を含める (FR-012) → T096
- [X] T092 [P] [US3] `test/integration/operation_logging_test.go` に、**ロックの取得・解放と適用した Description の記録** (FR-065)、DNS レコードの変更操作 (対象ゾーン・レコード名・操作種別・結果) の記録 (FR-089)、ゾーンが見つからない場合の `ResolvedZone` と対象 FQDN の記録 (FR-015)、`domainFilters` が空の場合の記録 (FR-019)、**`*` を含む場合の警告** (FR-023a)、ラベル上限到達の記録 (FR-087) のテストを書く → T096
- [X] T093 [P] [US3] `internal/observability/metrics_test.go` に、DNS レコードの変更操作の成否と件数が計測できること (FR-090)、**Prometheus 形式と OpenTelemetry 形式が同一の計測値を表すこと**、ゾーン名・レコード名・レコード値が**メトリクスのラベルに既定で含まれない**こと (FR-092) のテストを書く → T097
- [X] T094 [P] [US3] `internal/observability/trace_test.go` に、要求の受信から DPF 呼び出しまでが 1 トレースとして追跡できること (FR-091)、ゾーン名・レコード名・レコード値がトレースの属性に既定で含まれないこと (FR-092) のテストを書く → T098
- [X] T095 [P] [US3] `internal/observability/otlp_test.go` に、OTLP 送出が**既定で無効**であり送出先が設定された場合のみ有効になること、TLS 証明書の検証が既定で有効であること、**送出失敗で DNS の処理を停止させず、失敗をログに記録すること**のテストを書く (憲章 原則 V) → T099

### Implementation for User Story 3

- [X] T096 [US3] `internal/observability/log.go` と `internal/dpf/` に、エラー応答の全文記録・送った要求の記録・操作の記録・恒久的／一時的な失敗の分類の記録、`domainFilters` の `*` による緩和の警告を実装する。記録は `internal/dpf` の境界で行い、認証情報を伏せる (FR-010 / FR-023a / FR-065 / FR-088 / FR-088a / FR-088b / FR-089)
- [X] T097 [P] [US3] `internal/observability/metrics.go` に Prometheus 形式と OpenTelemetry 形式のメトリクスを実装する。両形式が同一の計測値を表すこと (FR-090)
- [X] T098 [P] [US3] `internal/observability/trace.go` に OpenTelemetry のトレースを実装する。要求の受信から DPF 呼び出しまでを 1 トレースとする (FR-091)
- [X] T099 [US3] `internal/observability/` に OTLP 送出 (ログ・メトリクス・トレース。gRPC と HTTP) を実装する。**既定で無効**とし、送出先が設定された場合にのみ有効化する。TLS 証明書の検証を既定で有効とする。送出失敗で DNS の処理を停止させない
- [ ] T100 [US3] `test/e2e/us3_diagnosis_test.go` に、5 種類の失敗 (無効なトークン / Secret 不在 / key 不在 / ゾーン不在 / DPF 到達不能) をログのみで判別できることの検証を書く (SC-009)。**(実装時の注記)** 無効なトークンの目印は DPF が 401 を返すことを前提とする。実環境で応答の状態コードを確かめる
- [X] T101 [US3] `CHANGELOG.md` の `Unreleased` に US3 が加えた利用者に見える挙動を記録する。**ログ・メトリクス・トレースとして出力する項目**と、OTLP 送出が既定で無効であることが対象 (憲章が「利用者に見える挙動」にテレメトリを含めるため)

**Checkpoint E**: 3 つのストーリーがすべて成立する

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: 配布物と品質ゲート。憲章が求める要件を満たす

### コンテナイメージと配布

- [ ] T102 [P] `Dockerfile` を作成する。ベースイメージは `scratch`。シェルとパッケージマネージャを含めない
- [ ] T103 [P] `Dockerfile` に CA 証明書を同梱する。`scratch` には証明書ストアがないため、DPF API への TLS 検証が成立する状態で配布する
- [ ] T104 [P] ビルドを ASLR 有効 (位置独立) で行うよう `Makefile` に設定する (CI の検証は T122)
- [ ] T105 [P] イメージの `/licenses/` に本体 (`LICENSE` / `NOTICE`) と依存のライセンス本文を `third-party/` 配下にモジュールパスの階層を保って同梱する。単一ファイルへ連結しない
- [ ] T106 [P] OCI アノテーション (`licenses` / `title` / `description` / `vendor` / `source` / `version` / `revision` / `created`) を付ける
- [ ] T107 `.github/workflows/release.yml` に、SBOM と provenance を referrers としてイメージへ紐づける処理を実装する。push と同時に行う

### チャートの既定拒否

- [ ] T108 [P] `test/integration/chart_policy_test.go` に、チャートの既定の検証を書く。**NetworkPolicy (憲章 v3.3.0)**: 既定の種類が k8s 標準であること、webhook ポートへの ingress が `networkPolicy.kubeAPIServerCIDRs` からに限られ**未設定なら描画が失敗すること**、cert-manager の namespace を送信元にしないこと、exposed ポートへの ingress が `networkPolicy.exposedFrom` に限られ全開放しないこと、**egress を既定で作らないこと**、`networkPolicy.egress` を与えた場合にその規則のみが egress になること、`networkPolicy.type=cilium` で CiliumNetworkPolicy (`fromEntities: kube-apiserver` / `host`) になること。**プロキシ**: `proxy.httpsProxy` を与えた場合にのみ `HTTPS_PROXY` / `NO_PROXY` (先頭に `$(KUBERNETES_SERVICE_HOST)`) を渡すこと。あわせて CPU・メモリの requests と limits があること、Pod Security Standards の `restricted` を満たし `privileged` / `hostNetwork` / `hostPID` / `hostIPC` / hostPath を既定で有効にしないこと、RBAC が用途 1 と用途 2 に閉じ Secret に `list`/`watch`/書き込み動詞とワイルドカードを含まないこと (D-08) → T109, T110
- [ ] T109 [P] `deploy/chart/templates/networkpolicy.yaml` を作成し、`values.yaml` に `networkPolicy` と `proxy` を加える (T108 の条件を満たす。plan.md の Technical Context)
- [ ] T110 [P] `deploy/chart/templates/deployment.yaml` に CPU・メモリの requests と limits の既定を追加する

### 利用者向け文書

- [X] T111 [P] `README.md` に**サポート範囲の注記**を憲章が定める文面のまま記載する。要約・言い換えをしない
- [X] T112 [P] `README.md` に自前 Helm チャートを用いたデプロイ手順を記載する。cert-manager 本体の導入は前提条件として扱う。**external-dns など他機構を経路として記載しない**
- [X] T113 [P] `README.md` に APIService の CA 証明書の配布経路、動作保証する cert-manager のバージョン (v1.21.2 以降)、ライセンスと著作権表示を記載する
- [ ] T114 [P] `README.md` に、kube-apiserver を NetworkPolicy の送信元として表現できないクラスタでの扱い (k8s 標準では CIDR を必須とすること、Cilium の選択) と、その残余をクライアント証明書の検証が担うことを記載する。**egress を制限する場合に許可すべき通信 (Kubernetes API server・DPF API・DNS・OTLP の送出先、プロキシ経由の場合はプロキシ) と、Cilium の `toFQDNs` / プロキシの設定例**を記載する。あわせて**運用上の注意** (同じゾーンを触るプログラムを `dpf-go` v0.6.0 以降にそろえること、ゾーンのラベルを 1 つ消費すること、ロック中も管理画面からの編集は止められないこと、使用済みの RDATA が期限まで残ること) を記載する
- [ ] T115 `README.md` と `CHANGELOG.md` の記載が食い違っていないことを確認する。食い違う場合は同じ変更のなかで `README.md` を直す (憲章「変更の記録」)
- [X] T116 [P] `CLAUDE.md` にディレクトリ構成・コーディング規約・頻用コマンドを記載する。憲章とは分離する

### CI 品質ゲート

- [X] T117 `.github/workflows/ci.yml` に憲章が求めるゲートを実装する: `betterleaks` による履歴のシークレット検査 / `gofmt -l ./...` が空 / SPDX ヘッダ / **ACME サーバの接続先の検査 (`scripts/check-acme-endpoints.sh`。本番環境と許可リスト外を拒否)** / `go build ./...` / `golangci-lint run` / `govulncheck ./...` / `go test ./...`
- [X] T118 `.github/workflows/ci.yml` に契約テスト (T055) を追加する
- [ ] T119 `.github/workflows/ci.yml` に既定設定での起動時挙動の検証を追加する (`domainFilters` 未設定で管理対象が空 / 必須設定の不足で異常終了 / webhook が平文で待ち受けない)。`cmd/webhook/main.go` の検証を兼ねる
- [ ] T120 `.github/workflows/ci.yml` に**クライアント証明書検証の検証**を追加する。証明書を提示しない接続と、集約層向け CA 以外で署名された証明書を提示する接続が、いずれも TLS ハンドシェイクで拒否されること (FR-002 / SC-010)
- [X] T121 `.github/workflows/ci.yml` にトークン取得元 namespace 判定の検証を追加する。`AllowAmbientCredentials` が `false` の要求が `ResourceNamespace` 以外を読めないこと、**RBAC 上は読める状態でも読まないこと**
- [ ] T122 `.github/workflows/ci.yml` に依存ライセンス検査と、イメージへのライセンス同梱・OCI アノテーション・`scratch`・ASLR の検証を追加する
- [X] T123 `.github/workflows/ci.yml` に、利用者に見える契約に対応するパス (`internal/solver/config.go` / `deploy/chart/**` / `contracts/solver-config.schema.json`) が変更された PR で `CHANGELOG.md` の更新を求める検査を追加する。**判定は補助でありレビューの代替ではない** (憲章のレビュー観点)
- [ ] T124 `.github/workflows/ci.yml` に実環境検証 (T012〜T015 / T075 / T076 / T087 / T100) を追加する。**実行環境として、CI 内に一時的な Kubernetes クラスタ (kind など) を立て、cert-manager v1.21.2 と、同じ CI で作ったイメージを用いた本チャートを導入する。**T076 のために、異なるロックオーナーを持つ配備を 2 つ (別の namespace) 用意する。クラスタは実行ごとに作り直し、終了時に破棄する。証明書の取得まで確かめる e2e には `ACME_SERVER` に Let's Encrypt のステージング環境を与え、本番環境の値を CI の設定に置かない。検証用ゾーンに対してレコードの追加・変更・削除を実行し、**並列実行しない** (`-p 1`)。後続の実行を待たせる
- [ ] T125 `.github/workflows/scheduled.yml` に `govulncheck` とイメージの脆弱性スキャンの定期実行を追加する
- [X] T126 CI で用いる Go / `golangci-lint` / `govulncheck` / `betterleaks` のバージョンを固定する

### 仕上げ

- [X] T127 依存ライセンスを実測し直し、許容リストの範囲内であることを確認する。憲章が求める再実測 (`misc/k8s` 採用後の構成)
- [ ] T128 `quickstart.md` の検証手順を通しで実行し、成功基準 SC-001〜SC-027 (削除した SC-002 を除く) を確認する

---

## Dependencies

### フェーズの順序

```
Phase 1 (Setup)
   ↓
Phase 2 (Foundational)  ← T012〜T015 の設計前提検証を最初に行う
   ↓
Phase 3 (US1 / P1) ──┐
   ↓                 │  US2 と US3 は US1 の完了後に着手するが
Phase 4 (US2 / P2)   │  互いには依存しない
   ↓                 │
Phase 5 (US3 / P3) ──┘
   ↓
Phase 6 (Polish)
```

### ストーリー間の依存

- **US1** は Phase 2 のみに依存する。単独で MVP になる
- **US2** は US1 の solver 実装 (T063〜T067) に乗る。namespace 決定の分岐を足す形
- **US3** は US1 の実装経路にログ・計測を足す形。US2 とは独立

### 重要な順序の制約

- **T013 / T015 → それ以外のすべて**。設計の前提であり、崩れた場合は research.md の
  D-13 / D-15 を見直す
- **テスト → 実装**。憲章 原則 III により、各実装タスクはその前のテストタスク
  (「→ Txxx」で対応を示す) が失敗することを確認してから着手する
- T018/T019 (dnsname / zonefilter) → T027 (challenge の plan) → T040 (apply)
- T035 (errors) → T036/T037 (ratelimit / client) → T040 (apply)
- T039 (record) → T064 (変更不要の分岐)
- T046/T047 (config / options) → T063 (solver の Initialize)
- T040 (apply) → T068 (Description)

---

## Parallel Execution Examples

### Phase 2 のテスト作成 (T016, T017, T020〜T024, T028〜T034, T043〜T045, T048, T050, T051)

いずれも別ファイルで依存がない (T022〜T024 は同一ファイルのため 1 人でまとめて書く)。
まとめて書き、すべて失敗することを確認してから実装に入る。

### Phase 3 のチャート (T069〜T072)

`deploy/chart/templates/` 配下の別ファイル。T062 を先に書いたうえで、solver の実装
(T063〜T068) と並行できる。

### Phase 6 の文書とイメージ (T102〜T106, T111〜T114, T116)

`Dockerfile` と `README.md` は独立。CI ゲート (T117 以降) は同一ファイルを触るため直列。

---

## Implementation Strategy

### MVP (最小の成立範囲)

**Phase 1 + Phase 2 + Phase 3 (US1)** = T001〜T078。

これだけで「cert-manager で DPF のドメインの証明書が取れる」という目的が達成される。
ClusterIssuer を 1 つ作れば、クラスタ内のどの namespace からでも証明書を発行できる。

### 段階的な提供

1. **MVP (US1)**: 単一のトークンをクラスタ全体で共有する構成で動く
2. **US2 を足す**: テナントごとに別のトークンを使える。マルチテナントのクラスタで必要
3. **US3 を足す**: 失敗の切り分けがログだけでできる。無人運用の前提
4. **Phase 6**: 配布物と品質ゲートを揃え、`main` へマージできる状態にする

### 変更の記録の扱い

憲章 v3.1.0 により、**利用者に見える挙動の変更は同じ変更のなかで記録する**。
各ストーリーの末尾に記録のタスクを置いてあるが、**それは「まとめて書く」ための枠ではない。**
ストーリーの途中で利用者に見える挙動を加えた時点で `CHANGELOG.md` を更新する。
末尾のタスクは、記録の漏れがないことを確かめる位置づけである。

`CHANGELOG.md` は T003 で先に作る。記録すべき変更が生じたときに受け皿がない状態を
作らないためである。

### 先に潰すべきリスク

- **T013 (タイムアウト後の収束)**: 1 回の処理は 60 秒を越えうる前提で設計している。
  呼び出し元のタイムアウト後も処理が完了し、再試行が変更なしで成功することが崩れると、
  証明書の取得が収束しない。崩れた場合は経路の見直しが要る
- **T015 (巻き添えの有無)**: ゾーン全体を置換する経路であるため、編集関数の誤りが
  ゾーン全体に及ぶ。ここが最大の危険

この 2 つは Phase 2 の先頭に置いてある。設計の前提そのものであり、後から崩れると
やり直しの範囲が大きい。
