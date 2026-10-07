<!-- SPDX-License-Identifier: Apache-2.0 -->

# Changelog

本ファイルの形式は [Keep a Changelog](https://keepachangelog.com/ja/1.1.0/) に、
バージョン番号は [Semantic Versioning](https://semver.org/lang/ja/) に従います。

> [!NOTE]
> `v1.0.0` に達するまでは、マイナーバージョンの更新に破壊的変更が含まれることがあります。
> cert-manager webhook API の互換性を壊す変更は MAJOR として扱います。

## [Unreleased]

### Added

- cert-manager の DNS01 solver を APIService (集約 API サーバ) として提供する。
  groupName は `acme.dns-platform.jp`、solverName は `iij-dpf`
- Issuer / ClusterIssuer の solver 設定: `endpoint` (既定 `https://api.dns-platform.jp/dpf/v1`。
  https のみ)、`tokenSecretRef` (`name` 既定 `dpf-token` / `key` 既定 `token` / `namespace`)、
  `domainFilters` (`*`・`*.<ドメイン>`・完全一致の 3 形式)。未知のフィールドは拒否する
- **`domainFilters` が空または未指定の場合、いかなるゾーンも変更しない。** `*` を指定した場合は
  警告をログに記録する
- トークンの Secret を読む namespace: ClusterIssuer は `tokenSecretRef.namespace`、省略時は
  本サービスの Pod の namespace。namespace 限定の Issuer はその namespace のみ
  (他の namespace を指す設定は失敗させる)
- 変更するゾーンは cert-manager が求めた委任先のゾーン (`ResolvedZone`) と一致する DPF の
  ゾーンに限る。DPF にそのゾーンが無ければ失敗する (親ゾーンへ読み替えない)
- ゾーン反映の Description は `by cert-manager(<ロックオーナー>)`
- **後片付け (CleanUp) は RDATA を削除しない。** 使用済みの RDATA と記録 (レコードのラベル
  `expire.acme.dns-platform.jp/<識別値>`) は期限 (既定 1 時間) を過ぎたのち、同じ名前に対する
  次の操作で回収される
- 配備単位の設定 (環境変数): `DPF_WEBHOOK_LOCK_OWNER` (必須。1〜32 文字)、
  `DPF_WEBHOOK_EXPIRE_GRACE`、`DPF_WEBHOOK_EXPIRE_THRESHOLD`、`DPF_WEBHOOK_OPERATION_DEADLINE`
  (既定 5 分)、`DPF_WEBHOOK_LOCK_TTL`、`DPF_WEBHOOK_LOCK_WAIT`、`DPF_WEBHOOK_LOG_LEVEL`、
  `POD_NAMESPACE`。必須設定が不足していれば起動しない
- Helm チャート: APIService (サービング証明書は cert-manager で発行し CA を注入)、Deployment
  (非 root・`restricted` を満たす。webhook はコンテナの 10443、healthz / metrics は 8080)、
  Service (443)、RBAC (Secret は `get` のみ、集約 API サーバの委譲認証、cert-manager による
  solver の呼び出し)。トークンの Secret は作らない
- コンテナイメージ (`ghcr.io/iij/cert-manager-iij-dpf-webhook`) と Helm チャート
  (`oci://ghcr.io/iij/charts`) をタグ付きリリースごとに公開する。イメージは `scratch` ベースで、
  バイナリは ASLR を有効にした静的な実行ファイル (C ライブラリ musl を静的にリンク)。CA 証明書を
  バイナリに備える。`/licenses/` に本体 (`LICENSE` / `NOTICE`) と依存のライセンス本文を置く。
  SBOM と provenance をレジストリの referrers として紐づける。UID 65532 で実行する
- Helm チャートの NetworkPolicy: 既定で作り、ingress を webhook ポートは kube-apiserver から、
  exposed ポートは `networkPolicy.exposedFrom` からに限る。egress は既定で制限せず、
  `networkPolicy.egress` に規則を与えた場合にのみその規則に限る。`networkPolicy.type` で
  `kubernetes` (既定。`networkPolicy.kubeAPIServerCIDRs` が**必須**で、無ければ導入に失敗する) と
  `cilium` (CiliumNetworkPolicy。kube-apiserver と kubelet の probe をエンティティで指定) を選ぶ
- Helm チャートの `proxy.httpsProxy` / `proxy.noProxy`: 与えた場合に `HTTPS_PROXY` / `NO_PROXY`
  を渡す。`NO_PROXY` の先頭に `$(KUBERNETES_SERVICE_HOST)` を置き、Kubernetes API はプロキシを
  経由させない
- Helm チャートの `resources`: CPU・メモリの requests と limits の既定 (requests 50m / 64Mi、
  limits 500m / 256Mi)
- Helm チャートの `tokenSecrets`: Secret の `get` を許す namespace を列挙して、namespace ごとの
  Role/RoleBinding のみで構成する。`tokenSecrets.namespaces` (既定 `[]`) に namespace 限定の
  Issuer を置く namespace を、`tokenSecrets.releaseNamespace` (既定 `true`) で本サービスの
  namespace を対象にする。ClusterIssuer を使わない構成では `false` にして本サービスの
  namespace に権限を与えない。全 namespace へ付与する ClusterRole は作らない
- トークンの取得の失敗の分類: Secret 不在・key 不在・値が空・読み取り権限なしは恒久的な失敗とし、
  再試行しない。Kubernetes API への到達失敗などは一時的な失敗とし、1 回の読み出しにつき
  3 回まで試行する。分類はログの `failure` (`permanent` / `temporary`) に記録する
- トークンは要求のたびに Secret から読む。ローテーションや Secret の作成・修正は再起動なしに反映される
- ログ (JSON、標準出力。レベルは `DPF_WEBHOOK_LOG_LEVEL`)。要求ごとに `uid`・`action`・`fqdn`・
  `resolvedZone` を付ける。主な記録:
  - `DPF API request`: DPF へ送った要求 (`method`・`path`・`zoneID`・`query`・`body`)。ゾーン全体の
    一括置換は本文の代わりに送ったレコードの総数 (`records`) を記録する
  - `DPF API error response`: DPF のエラー応答 (`status` と本文の全文。`request_id` を含む)
  - `Kubernetes API request`: トークンの Secret の取得 (`method`・`path`・`key`)
  - `zone lock acquired` / `zone lock released`: ゾーンロックの取得と解放
  - `DNS record change`: 変更操作 (`zone`・`name`・`operation` (create / update / delete)・加除した
    RDATA とラベル・`records`・`description`・`result`)
  - `challenge failed`: 失敗と分類 (`failure`: permanent / temporary)。ラベル上限への到達は
    `record label limit reached` を併せて記録する
  - トークンの値は記録しない。DPF の応答などに現れた場合は `[REDACTED]` に置き換える
- cert-manager へ返すエラー (Challenge の status に表示される) には、DPF のエラー応答の本文と
  送った要求の内容を含めない。全文はログに残す (`uid` で対応づける)
- メトリクス (`/metrics` の Prometheus 形式と OTLP): `dpf_webhook_challenge_requests_total` (要求) と
  `dpf_webhook_dns_record_changes_total` (ゾーンへの反映を要した変更操作)。ラベルは `action`
  (present / cleanup) と `result` (success / failure) のみで、ゾーン名・レコード名・値を含めない
- トレース (OTLP): 要求ごとの span (`iij-dpf.present` / `iij-dpf.cleanup`) と、その子として DPF の
  呼び出しごとの span (`dpf <メソッド>`)。属性にゾーン名・レコード名・値を含めない
- OTLP 送出 (ログ・メトリクス・トレース。gRPC と HTTP)。**既定で無効**であり、
  `OTEL_EXPORTER_OTLP_ENDPOINT` (または信号ごとの `OTEL_EXPORTER_OTLP_<TRACES|METRICS|LOGS>_ENDPOINT`)
  を与えた場合にのみ送出する。方式は `OTEL_EXPORTER_OTLP_PROTOCOL` (`grpc` / `http/protobuf`。
  省略時 `http/protobuf`)。送出先の TLS 証明書は検証する。送出の失敗は `telemetry export failed`
  として標準出力へ記録し、処理は続ける。チャートでは `otlp.endpoint` / `otlp.protocol` で与える

### Notes

- **同じゾーンを操作する他のプログラムも `dpf-go` v0.6.0 以降にそろえること。** v0.5.0 以前は
  ゾーンロックの状態を SOA のラベルに置くため、混在すると排他が成立しない
- ゾーンロックはゾーンのラベルを 1 つ恒久的に消費する (`lock.dpf-go`)。利用者が付けられる
  ゾーンのラベルは 9 個までになる
- ゾーンロックの保持中も、管理画面や他ユーザからのレコードの編集は止められない
