# Implementation Plan: DPF DNS01 Challenge Solver

**Branch**: `001-dpf-dns01-solver` | **Date**: 2026-09-18 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-dpf-dns01-solver/spec.md`

## Summary

cert-manager の DNS01 solver を、Kubernetes の APIService (集約 API サーバ) として実装する。
cert-manager が提供する webhook の枠組み (`pkg/acme/webhook/cmd`) に `webhook.Solver` の
実装を与え、DPF 固有の処理を `github.com/iij/dpf-go` 経由で行う。

技術的な要点は 4 つある。

1. **認証はフレームワークが担う。** `genericoptions.NewRecommendedOptions` の委譲認証が
   `kube-system/extension-apiserver-authentication` の CA でクライアント証明書を検証する。
   集約層を経由しない接続は TLS ハンドシェイクで落ちる。独自実装は不要。
2. **ゾーンの排他と反映は `dpf-go` の `utils.ZoneApplier`** に委ねる。公開レコードを読み、
   編集関数の結果でゾーンを一括置換して反映する。排他の取得・自動延長・解放まで内包する。
   v0.6.0 で排他の状態はゾーンのラベルに移り、一括置換と反映が排他に影響しなくなった。
   **2 者の同時取得は、ユーザ (アクセストークン) をまたいで起こらない。** 一方、保持中の
   他ユーザ・管理画面からの編集は止められない。排他の差し替え (`utils.WithLocker`) は
   **行わない**。外部の仕組みに替えると、同時取得を防げる範囲がその仕組みの利用者に狭まる。
3. **同一チャレンジコードの共有**は、**期限 (expire) による回収**で解決する。
   後片付けは RDATA を削除せず、期限を過ぎたものを次の操作の際に回収する。
   **誰も他人の RDATA を消さない**ため、保持者の識別・集合演算・再試行をまたいだ
   識別値の安定性が、いずれも不要になる。記録はチャレンジレコードのラベルに置き、
   チャレンジコード 1 つにつき 1 つ。ラベルは DNS に公開されない。
4. **トークンは要求ごとに決まる**ため DPF クライアントを出所ごとにキャッシュし、
   レート制限だけを共通の RoundTripper で全体に効かせる。
5. **1 回の処理は要求の上限 60 秒に達することを前提とする** (出典は research.md D-15)。
   変更系 API は非同期で 1 回 10 秒以上かかり、同一ゾーンでは並行できない。排他の取得だけで
   完了を待つ変更が 3 回動き、回数は `dpf-go` の実装で決まる。そこで処理は呼び出し元の
   タイムアウト後も完了まで進め、**再試行が望む最終状態を見つけて変更を行わずに成功する**
   ことで収束させる。操作期限 (既定 5 分) は、取り残された処理の上限として置く。

調査の詳細と決定の根拠は [research.md](./research.md) にある。

## Technical Context

**Language/Version**: Go 1.27 (`go.mod` の go ディレクティブは `1.27`)。
cert-manager v1.21.2 の `go.mod` は `go 1.26.0` であり矛盾しない。

**Primary Dependencies**:

| 依存 | 用途 |
|---|---|
| `github.com/cert-manager/cert-manager` v1.21.2 | `pkg/acme/webhook/cmd` (起動) / `webhook.Solver` (契約) |
| `github.com/iij/dpf-go` **v0.6.0** | DPF API。`utils` の Client / Mutex / **ZoneApplier** / ゾーン取得 |
| `github.com/iij/dpf-go/misc/k8s` | Kubernetes Secret からトークンを取る TokenProvider |
| `github.com/miekg/dns` | ドメイン名の正規化・包含判定・ラベル分割 |
| `k8s.io/client-go` | Secret の取得 (`Initialize` が渡す rest.Config から生成) |
| `k8s.io/apiserver` **v0.37.1** | 集約 API サーバ基盤 (cert-manager 経由で間接依存)。misc/k8s と版を揃えるため cert-manager の前提 (v0.36.2) から上げる (research.md D-19) |

`misc/vault` / `misc/aws` / `misc/azure` / `misc/gcp` は**使わない**。
HashiCorp 系の MPL-2.0 依存は入らない。`misc/k8s` は `k8s.io/client-go` (Apache-2.0) のみを
追加するが、集約 API サーバ基盤のために既に必要であり追加の負担はない。
`misc/k8s` は独立モジュールであり本体と別に採番される。タグ付きの版が出た時点で固定し直す。

**Storage**: 永続ストレージを持たない。状態は DPF のゾーン (チャレンジ用 TXT レコードの
RDATA とラベル、およびゾーンのラベルによるロック) に置く。

**Testing**: `go test ./...`。DPF API は `dpf.RecordsApi` / `dpf.ZonesApi` インタフェースで
差し替える (`dpf.ZonesApi` は v0.6.0 で `GetZoneLabels` / `PutZoneLabels` を含む)。契約テストは `webhook.Solver` の振る舞いに対して書く。実環境検証は CI で
検証用ゾーンに対して行う (憲章の要求)。

**証明書の取得まで確かめる e2e の ACME サーバ**: **Let's Encrypt のステージング環境**
(`https://acme-staging-v02.api.letsencrypt.org/directory`) を用いる (research.md D-17)。
接続先は環境変数 `ACME_SERVER` で与え、**未設定の場合はステージング環境を用いる**
(本番環境へ既定で接続しない。憲章「ACME サーバの利用」)。テストと CI が用いてよい接続先は
次の許可リストに限り、それ以外 (特に下記の本番環境の一覧) を指していれば CI を失敗させる。

| 区分 | 接続先 |
|---|---|
| **許可** (テストと CI) | `https://acme-staging-v02.api.letsencrypt.org/directory` |
| **本番環境 (拒否)** | `https://acme-v02.api.letsencrypt.org/directory` (Let's Encrypt) |
| | `https://dv.acme-v02.api.pki.goog/directory` (Google Trust Services) |
| | `https://acme.zerossl.com/v2/DV90` (ZeroSSL) |
| | `https://api.buypass.com/acme/directory` (Buypass) |

検査は `scripts/check-acme-endpoints.sh` で行い、対象は本プロジェクトのテストと CI の設定
(`test/`、`.github/`、CI が用いる values) に限る。**利用者向けの文書 (README・quickstart の
導入例) は対象外**とする (憲章の同節が対象外と定めるため)。許可リストを持つのは、本番環境の
一覧に無い認証局の本番環境を見落とさないためである (原則 VI)。

**Target Platform**: Linux コンテナ (`scratch` ベース)。Kubernetes 上で APIService として動作。

**NetworkPolicy** (憲章 v3.3.0): チャートの `networkPolicy.type` で種類を選ぶ。

| 種類 (`networkPolicy.type`) | webhook ポート (コンテナの 10443) への ingress の送信元 | exposed ポート (8080) への ingress の送信元 |
|---|---|---|
| `kubernetes` (**既定**。`networking.k8s.io/v1` の NetworkPolicy) | `networkPolicy.kubeAPIServerCIDRs` (**必須**。未設定なら描画を失敗させる) | `networkPolicy.exposedFrom` (NetworkPolicyPeer の一覧。既定は空) |
| `cilium` (`cilium.io/v2` の CiliumNetworkPolicy) | `fromEntities: [kube-apiserver]` | `fromEntities: [host]` (kubelet の probe) と `networkPolicy.exposedFrom` |

- egress の制限は既定で作らない。`networkPolicy.egress` に規則 (種類に応じた egress の規則の
  一覧。Cilium では `toFQDNs` も書ける) を与えた場合にのみ、egress をその規則に限る
- 本サービスが必要とする egress: Kubernetes API server (443/TCP)、DPF API
  (`api.dns-platform.jp` の 443/TCP)、DNS (53/UDP・TCP)、OTLP の送出先 (設定した場合)。
  HTTP プロキシを経由する場合は DPF API と OTLP の代わりにプロキシへの egress を許す
- HTTP プロキシは `proxy.httpsProxy` / `proxy.noProxy` で与え、環境変数 `HTTPS_PROXY` /
  `NO_PROXY` としてコンテナへ渡す。DPF API・Kubernetes API・OTLP の各クライアントはこの
  環境変数に従う。Kubernetes API をプロキシ経由にしないため、`NO_PROXY` の先頭に
  `$(KUBERNETES_SERVICE_HOST)` を常に置く

**Project Type**: 単一の HTTP サービス (集約 API サーバ)。CLI・ライブラリとしての公開はしない。

**Performance Goals**: DPF の変更系 API は非同期であり、ジョブ 1 回の完了に **10 秒以上**を要する。
同一ゾーンへのレコード操作は並行できないため待ち時間は積算される。
1 チャレンジあたり、完了を待つ変更が 4 回 (排他の取得の 3 回と反映 1 回)、完了を待たない
解放が 1 回動く。回数は `dpf-go` の実装で決まり、本サービスは制御しない。ゾーン反映の
所要時間は、編集していないレコードも含めたゾーン全体のレコード数に影響される。
**総処理時間は 60 秒を越えうる前提で設計する。**

本サービスが制御できるのは、変更が不要な操作で変更 API を 0 回に抑えること (SC-025 /
SC-026)、タイムアウト後の再試行で収束すること (SC-027)、自身のオーバーヘッド (SC-003) で
あり、それを基準とする。DPF API への送出はプロセス全体で 5 req/s (burst 10)、同時実行 5 を
超えない (`dpf-go` の既定)。

**Constraints**:

- webhook ポート 443 (TLS のみ / クライアント証明書検証必須)、exposed ポート 8080
  (healthz + metrics のみ、認証なし)
- 本サービスの apiserver の `--request-timeout` は既定 60 秒。延ばしても集約層が
  60 秒のままなら実効的な上限は変わらないため、既定のままとする
- Description は 80 オクテット以内、ロックオーナーは 1〜32 文字 (`dpf-go` v0.6.0 の上限)
- 排他はゾーンのラベルを 1 つ恒久的に消費する。ゾーンのラベルが上限に達していれば取得できない
- v0.5.0 以前の `dpf-go` を用いるプログラムと同じゾーンを操作すると排他が成立しない
- ゾーンロックの TTL は既定 **2 分** (`dpf-go` の既定 15 分から短縮)。
  1 回の操作を覆い、かつ中断した保持者が他をブロックする時間を抑える。
  `Mutex` は保持期間の 1/3 を延長の間隔として申告するため 40 秒ごとに延長される
- 期限の記録はラベルに置くため、収容数は **10 件**。本サービス以外のラベルと枠を共有する
- 期限の猶予は既定 **1 時間**。長いほど使用済みの記録が残り、ラベル上限に近づく
- 期限の更新は、残りが**閾値 (猶予の 1/2、既定 30 分) を下回る場合のみ**。
  再試行が重なっても無駄な反映でゾーンを占有しない
- **望む最終状態が現状と一致する操作は変更 API 0 回** (`CleanUp` で回収対象が無い場合、
  および閾値の期間内の `Present` の再試行)
- **操作全体に期限を設ける。既定 5 分であり、60 秒より長くする。**
  `Present` / `CleanUp` には context が渡されないため、呼び出しの中断を観測できない。
  要求の上限 60 秒 (`k8s.io/apiserver@v0.36.2/pkg/server/config.go:453` の `RequestTimeout`
  既定。**集約層と本サービス自身の両方に効く**) を越えても、apiserver はハンドラの
  goroutine を止めない。この性質を利用して処理を完了まで進め、再試行で収束させる。
  期限は取り残された処理の上限であり、**要求の context から派生させない**
- 排他を取得できない場合は 5 秒間隔で待つ (`WithLockWait`)。待機の上限は操作期限
- 反映が完了したのちに解放だけが失敗した場合も、要求の失敗として返す。再試行は
  変更不要と判定されて成功する
- テストと CI が通信する ACME サーバは、上記の許可リスト (Let's Encrypt のステージング環境) に
  限る。本番環境の ACME サーバを用いない
- 反映は `utils.ZoneApplier.Apply` を用いる。**`PatchZoneAtomicChanges` を直接呼ばない**
  (`overwrite_soa` / `overwrite_zone_apex_ns` の既定が `true` であり、自前で組むと
  SOA と Zone Apex の NS が意図せず置き換わる)

**Scale/Scope**: 1 クラスタあたり数十の Certificate。同一 ACME アカウントを共有する
クラスタ数は数台を想定。同時に走るチャレンジは同一ゾーンに対して数件。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

憲章 **v3.2.0** に対する評価。

### Core Principles

| 原則 | 評価 | 根拠 |
|---|---|---|
| I. cert-manager Webhook 契約への準拠 | **PASS** | 契約は `webhook.Solver` の実装のみ。独自エンドポイントを追加しない。契約テストを仕様の実行可能な表現とする |
| II. プロバイダ境界の分離 | **PASS** | `dpf-go` の型とエラーを DPF クライアント層の内側に閉じる。Solver 実装はインタフェース経由で呼ぶ (下記 Project Structure) |
| III. テストファースト | **PASS** | tasks 生成時に、各 FR に対するテストを実装タスクより前に置く |
| IV. DNS 変更の安全性 | **PASS** | 冪等性は RDATA の集合操作として実現。範囲判定は `domainFilters` を書き込み直前に評価。fail closed。ゾーン置換は `utils.ZoneApplier` 経由に限り、編集関数が対象以外をそのまま返すことを単体テストで固定する (下記 Post-Design 再評価) |
| V. 可観測性と運用性 | **PASS** | 構造化ログを標準出力へ常時。OTLP は既定無効。metrics は Prometheus + OTLP。トークンは出力しない |
| VI. Default-Deny | **PASS** | `domainFilters` 空 = 何も変更しない。ロックオーナー未設定 = 起動中止。未知の config フィールドは失敗 |

### 技術・配布制約

| 制約 | 評価 | 備考 |
|---|---|---|
| 実装言語 Go / go 1.27 | PASS | |
| Apache-2.0 / 全 Go ファイルに SPDX ヘッダ | PASS | CI で検証 |
| 依存ライセンス許容リスト | **要再実測** | `misc/*` を使わないため MPL-2.0 は入らない見込み。実測は実装時に取り直す (憲章の要求) |
| `gofmt` / `golangci-lint` / `govulncheck` | PASS | |
| DPF API は `dpf-go` を用いる | PASS | 自前 HTTP クライアントを書かない |
| 呼称は「IIJ DNSプラットフォームサービス」/「DPF」 | PASS | |
| ドメイン名は `miekg/dns` で扱う | PASS | ゾーン取得は `utils.GetZoneFromName` (内部で `dns.IsSubDomain` / `dns.CountLabel`) の結果が `ResolvedZone` と正規化名で一致することを確かめる。`ResolvedFQDN` が配下にあることは `dns.IsSubDomain` で判定する。`domainFilters` の判定も `miekg/dns` で実装し `strings` を使わない |
| APIService として実装 / 集約層から呼ばれる | PASS | |
| クライアント証明書検証 / 平文で待ち受けない | **要対応** | 平文で待ち受けないこと、委譲認証による証明書の検証はフレームワークが提供する。**ただしフレームワーク (`k8s.io/apiserver`) はクライアント証明書を任意 (`tls.RequestClientCert`) とし、証明書の無い接続をハンドシェイクで拒否しない** (research.md §2.2 の記述は誤り)。実現方法を調査する (T120) |
| RBAC は用途 1 と用途 2 に閉じる | PASS | research.md D-08 で一覧を確定 |
| NetworkPolicy (v3.3.0) | PASS | 種類は k8s 標準 (既定) と Cilium。ingress は kube-apiserver と probe・スクレイプに限る。egress は既定で作らず、利用者が規則を与える (Technical Context) |
| `scratch` / 非 root / `restricted` | PASS | |
| 変更の記録 (`CHANGELOG.md`、v3.1.0) | PASS | 各ユーザーストーリーの末尾に記録のタスクを置き、CI で契約に対応するパスの変更時に更新を求める |
| 実環境での検証 (検証用ゾーン、直列実行) | PASS | e2e を `-p 1` で直列に実行する。本番ゾーンを使わない |
| ACME サーバの利用 (v3.2.0) | PASS | e2e はステージング環境を用い、未設定時もステージング環境になる。許可リスト外と本番環境の一覧を CI で拒否する (Technical Context) |

### 逸脱

**なし。** Complexity Tracking は空。

憲章 v3.0.1 で ClusterIssuer 用の既定 namespace の文言が改められ、本仕様と一致した。
v3.1.0 (変更の記録) と v3.2.0 (ACME サーバの利用) の追加も本計画で満たす。
未解消の差分はない。

### Post-Design 再評価

Phase 1 の設計 (data-model.md / contracts/) を経ても上記の評価は変わらない。
新たな逸脱は生じていない。特に確認した点:

- 期限の記録をチャレンジレコードのラベルに置くことは、憲章が原則 IV で求める
  「範囲の限定」を破らない。**新たなレコードを作らず**、対象ゾーンは `domainFilters` の
  判定を通ったゾーンであり、変更するのはチャレンジレコード 1 つに限られる。
  他者が付けたラベルは接頭辞で区別して維持し、**対応するラベルを持たない RDATA は
  回収しない。**
- **使用済みの RDATA が期限まで残る点**は、憲章 原則 IV に反しない。原則 IV が求めるのは
  冪等性と範囲の限定であり、即時の削除ではない。ただし cert-manager の `CleanUp` の
  文面からは外れるため、利用者の判断として Assumptions に記録した。
- **ゾーン全体の置換を用いるが、原則 IV は守られる。** `utils.ZoneApplier` の編集関数は
  入力と出力が同じ型であり、対象以外の要素をそのまま返す限り RDATA・TTL・ラベル・コメントは
  失われない。SOA と Zone Apex の NS は `false` 固定で取り込まれない。
  **編集関数を小さく保ち、対象以外に触れないことを単体テストで固定する**ことで担保する。
- 排他に参加しない変更者の未反映の編集が失われる点は、憲章が既に受け入れている
  「ロックに参加しない機械的な変更者」の扱いの範囲内である。v0.6.0 では保持中の
  他ユーザ・管理画面からの編集も止められなくなったため、この窓は v0.5.0 の想定より広い
  (research.md D-13)。
- ロックオーナーを必須設定としたことは原則 VI と整合する。`dpf-go` の既定
  (ホスト名・PID・乱数) はインスタンスごとに一意だが、どのクラスタの配備かを読み取れない
  ため、既定に頼らない。

## Project Structure

### Documentation (this feature)

```text
specs/001-dpf-dns01-solver/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   ├── solver-config.schema.json   # Issuer/ClusterIssuer の config スキーマ
│   ├── apiservice.md               # APIService としての契約
│   └── dpf-records.md              # DPF 上のレコードとラベルの配置の契約
├── checklists/
│   └── requirements.md
└── tasks.md             # /speckit-tasks の出力 (本コマンドでは作らない)
```

### Source Code (repository root)

```text
cmd/
└── webhook/
    └── main.go                  # cmd.RunWebhookServer(groupName, solver) を呼ぶだけ

internal/
├── solver/                      # webhook.Solver の実装 (契約の境界)
│   ├── solver.go                # Name / Present / CleanUp / Initialize
│   ├── config.go                # config のパース・既定値・未知フィールドの拒否
│   └── options.go               # 配備単位の設定 (ロックオーナー、猶予、期限)
├── challenge/                   # ドメインロジック (DPF に依存しない)
│   ├── plan.go                  # 現在値 + 要求 → 変更計画
│   ├── expire.go                # 期限の記録 (ラベル) の符号化・復号・回収の判定
│   └── digest.go                # 対象の識別値 (チャレンジコードのダイジェスト)
├── zonefilter/
│   └── filter.go                # domainFilters のパースと判定 (miekg/dns のみ使用)
├── dnsname/
│   └── name.go                  # 正規化名の型。境界で一度だけ正規化する
├── dpf/                         # DPF クライアント層 (原則 II の境界)
│   ├── client.go                # utils.NewClient のラップ。出所ごとのキャッシュ
│   ├── ratelimit.go             # 共通 RoundTripper (プロセス全体で 1 つ)
│   ├── zone.go                  # ResolvedZone と一致するゾーンの取得 (longest match の結果を確かめる。D-18)
│   ├── record.go                # 排他を取らない公開レコードの読み出し (変更の要否の事前判定。D-11 の 2a)
│   ├── apply.go                 # utils.ZoneApplier のラップ。編集関数の受け渡し
│   └── errors.go                # DPF のエラーを本プロジェクトのエラー型へ変換
├── tokensource/
│   └── provider.go              # misc/k8s の TokenProvider を要求ごとに組み立てる
├── observability/
│   ├── log.go                   # 構造化ログ (標準出力常時 + OTLP 任意)
│   ├── metrics.go               # Prometheus + OTLP
│   ├── trace.go                 # OTLP
│   └── exposed.go               # 8080 の healthz / metrics サーバ
└── version/
    └── version.go

deploy/
└── chart/                       # 自前 Helm チャート
    ├── Chart.yaml
    ├── values.yaml              # 既定値は拒否側
    └── templates/               # APIService / Deployment / RBAC / NetworkPolicy / Service

test/
├── contract/                    # webhook.Solver の契約テスト
├── integration/                 # DPF をモックした結合テスト、およびチャートの描画結果の検証
└── e2e/                         # 実 DPF に対する検証 (CI、検証用ゾーン、直列実行)
    └── acmeserver/              # e2e が用いる ACME サーバの解決 (既定はステージング環境。research.md D-17)

scripts/
├── check-spdx.sh                # SPDX ヘッダの検査
├── check-domain-string.sh       # ドメイン名の判定に strings が使われていないことの検査
├── check-acme-endpoints.sh      # テストと CI の ACME サーバの接続先の検査 (許可リスト外を拒否)
├── check-acme-endpoints_test.sh
└── testdata/acme-endpoints/     # 上記の検査の入力

.github/
├── pull_request_template.md     # テストが実装に先行したことを追跡する欄 (憲章 原則 III)
└── workflows/                   # ci.yml / release.yml / scheduled.yml
```

**Structure Decision**: 単一の Go モジュール。憲章の原則 II (プロバイダ境界の分離) を
ディレクトリで表現する。

- `internal/challenge` と `internal/zonefilter` は **DPF を知らない。** 現在の状態と要求を
  受け取り、あるべき変更を返す純粋なロジックに寄せる。ここが FR の大半に対する単体テストの
  対象になる。
- `internal/dpf` が `dpf-go` の型・エラー・リトライ・レート制限・ロックを閉じ込める。
  上位は `internal/dpf` が公開するインタフェースのみを見る。
- `internal/solver` は cert-manager の契約 (`webhook.Solver`) に対する薄い変換層に留める。
  契約テストはここに当てる。

`cmd/webhook/main.go` を最小にするのは、フレームワークの起動処理 (`RunWebhookServer` は
`os.Exit` を呼びうる) をテスト対象から外すため。

## Complexity Tracking

> Constitution Check に逸脱がないため空。
