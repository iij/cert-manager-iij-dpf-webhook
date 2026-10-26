# Quickstart: DPF DNS01 Challenge Solver の検証

**対象**: [spec.md](./spec.md) / 設計は [plan.md](./plan.md)、契約は [contracts/](./contracts/)

機能が端から端まで動いていることを確かめる手順。実装の詳細は書かない。
各検証は spec.md の成功基準 (SC) に対応させている。

---

## 前提

| 必要なもの | 備考 |
|---|---|
| Kubernetes クラスタ | APIService を登録できる権限 |
| cert-manager v1.21.2 以降 | 本サービスは cert-manager 本体を同梱しない |
| DPF の契約とアクセストークン | 対象ゾーンのレコードを操作できるもの |
| **検証用ゾーン** | 破壊的操作を行ってよいもの。本番ゾーンを使わない |
| Go 1.27 以上 | ビルドと単体テスト |

---

## 1. ビルドと単体テスト

```bash
go build ./...
go test ./...
```

`internal/challenge` と `internal/zonefilter` は DPF を知らないため、
FR の大半はここで DPF なしに検証できる。

```bash
# domainFilters の判定 (SC-015, SC-016, SC-017)
go test ./internal/zonefilter/...

# RDATA とメタデータの加除・冪等性・期限切れ回収 (SC-005, SC-019, SC-020, SC-022, SC-023, SC-024)
go test ./internal/challenge/...
```

**期待**: すべて成功する。特に次が確認できること。

- `domainFilters` が空のとき、変更計画が空になる
- `*example.jp` のようなラベル境界に揃わないパターンがパースで拒否される
- 同一の `Present` を 2 回適用しても最終状態が変わらない
- 同一の `CleanUp` を 2 回適用しても他の保持者の記録が消えない
- 本サービス以外の RDATA が維持され、TTL が変わらない

## 2. 契約テスト

```bash
go test ./test/contract/...
```

`webhook.Solver` の振る舞いを固定する。`Name()` が `iij-dpf` を返すこと、
未知の config フィールドで失敗すること、`AllowAmbientCredentials` による
namespace の決定が仕様どおりであること (SC-008)。

## 3. 起動時検証 (Default-Deny)

ロックオーナーを与えずに起動する。

```bash
./webhook --secure-port=443
```

**期待**: 起動せずに異常終了し、ロックオーナーが必要であることが判別できるログが出る。
33 文字以上のロックオーナー、または英数字と `.` `-` `_` 以外を含むロックオーナーを与えた場合も同様に異常終了する。

## 4. モック DPF に対する結合テスト

```bash
go test ./test/integration/...
```

DPF API を差し替えて、変更の順序 (ロック取得 → 読み出し → 加除 → 反映 → 解放) と、
ロック外で読んだ値を使わないことを確認する。`dpf.ZonesApi` の差し替えは v0.6.0 で加わった
`GetZoneLabels` / `PutZoneLabels` も実装する (排他の状態がゾーンのラベルに置かれるため)。

あわせて、ロック取得後に読んだ状態が望む最終状態であれば、編集関数が `ErrSkipApply` を返し
反映 (`PatchZoneAtomicChanges`) が呼ばれないことを確認する。

**期待**: 読み出しがロック取得より後に起きていること。反映がロック解放より前に
起きていること。

## 5. 実環境での検証 (CI・検証用ゾーン)

憲章は `main` へのマージ前に実際の DPF に対する検証を要求している。
**この検証を並列に実行しない。** 検証用ゾーンが共有されるため。

```bash
export DPF_API_TOKEN=...          # 検証用の契約のトークン
export DPF_TEST_ZONE=example.test # 破壊的操作を行ってよいゾーン
# 証明書の取得まで確かめる e2e の ACME サーバ。未設定時もステージング環境になる
export ACME_SERVER=https://acme-staging-v02.api.letsencrypt.org/directory
go test -tags=e2e -p 1 ./test/e2e/...
```

**本プロジェクトの e2e では本番環境の ACME サーバを用いない** (憲章「ACME サーバの利用」)。
`ACME_SERVER` が許可リスト (plan.md の Technical Context) の外を指していれば、CI の検査
(`scripts/check-acme-endpoints.sh`) と e2e の開始時の検査の双方で失敗する。
下記 §7 の ClusterIssuer の例は利用者の導入手順であり、本番環境を指してよい。

確認する項目 (research.md §4 に対応):

| # | 確認すること | 対応 |
|---|---|---|
| 1 | レコードの追加・変更・削除が反映される | 憲章の実環境検証 |
| 2 | 同一の変更を再適用しても最終状態が変わらない | SC-005 |
| 3 | **編集関数がそのまま返したレコードが一切変更されない** (RDATA・TTL・ラベル・コメント) | 憲章 原則 IV |
| 4 | SOA と Zone Apex の NS が置き換わらない | 憲章 原則 IV |
| 5 | RDATA を 1 件も持たないレコードが作れない (`minItems: 1`) | レコードごと削除する根拠 |
| 6 | **同一トークンの 2 プロセス間で排他が成立する** | SC-011 |
| 6a | **別トークンの 2 プロセス間で排他が成立する** (同時取得が起こらない) | D-16 |
| 6b | **アクセストークンでゾーンのラベルを読み書きでき、ロックオーナーが値として受け付けられる** | research.md §4 1b |
| 7 | 期限切れ排他の奪取が成立する | |
| 8 | **排他の取得の各段・反映・解放それぞれの所要時間、1 チャレンジの総処理時間** | D-15 |
| 8a | **1 回の処理が 60 秒を越えても、再試行を経て証明書の取得が完了する** | SC-027 |
| 9 | **操作期限に達したとき、操作期限を越えて DPF への書き込みが行われない。**排他は TTL の満了で失効する | SC-004 |
| 9a | **タイムアウト後に処理が成功し再試行が来ても、RDATA が重複せず、変更も行われない** | SC-005 / SC-025 |
| 9b | **望む最終状態が現状と一致する操作で、変更 API が 1 回も呼ばれない** (`CleanUp` / 閾値内の `Present` 再試行の双方) | SC-025 / SC-026 |
| 9c | **対応するラベルを持たない RDATA が回収されない** | 憲章 原則 IV |
| 10 | 排他の一時レコード (`_dpf-go-lock`) が公開されたまま残らない | |

**8a と 3 を最初に実行する。** 8a はタイムアウトを前提とした収束が本設計の前提であり、
崩れた場合は経路の見直しが必要になる。**3 はゾーン全体の置換を採る判断の前提そのものであり、
崩れた場合はゾーン全体を壊す。**

**ラベルに関する制約は検証を要しない。** 件数 10・値 1〜63・キーの書式 `prefix/name`・
prefix 253・name 63・文字種は、`openapi.json` と DPF の共通ルールで確定している
(research.md §2.5)。ただしゾーンのラベルをロックオーナーの値で書けること (6b) は、
トークンの権限に依存するため確認する。

**8 の確認方法**: 排他の取得の 3 段 (一時レコードの作成・ゾーンのラベルの書き込み・
一時レコードの取り消し)、反映、解放のそれぞれの所要時間を記録する。**回数は基準にしない**
(`dpf-go` の実装で決まるため)。記録は操作期限の既定値 (5 分) の妥当性を判断する
根拠にする。あわせてゾーンのレコード数と所要時間の関係を確認する。

**8a の確認方法**: 所要時間が 60 秒を越えるゾーン (レコード数の多いゾーン) で証明書を取得する。
集約層がタイムアウトを返したのちも本サービスの処理が完了すること、再試行が変更 API を
呼ばずに成功すること、先の処理が走行中に来た再試行が排他を待ち反映を省くことを、
ログと DPF の変更履歴で確認する (`k8s.io/apiserver@v0.36.2/pkg/server/config.go:453` の
`RequestTimeout` 既定 60 秒は、集約層と本サービス自身の双方に効く)。

**3 の確認方法**: 検証用ゾーンに本サービスと無関係なレコードを数件置く。TTL が null の
もの、ラベルが付いたもの、コメントが付いたものを含める。提示と後片付けを行い、
それらが RDATA・TTL・ラベル・コメントのいずれも変わらないことを確認する。
**ゾーン全体を置換する経路であるため、ここが最大の危険である。**

### 運用上の注意 (dpf-go v0.6.0)

- **同じゾーンを操作する他のプログラムも `dpf-go` v0.6.0 以降にそろえる。**v0.5.0 以前は
  排他の状態を SOA のラベルに置くため、混在すると排他が成立しない。検証用ゾーンを
  他のツール (例: `external-dns-iij-dpf-webhook`) と共有する場合は特に注意する。
- 排他はゾーンのラベルを 1 つ恒久的に消費する (`lock.dpf-go`)。検証用ゾーンのラベルが
  上限に達していないことを確認する。
- 排他の保持中も、管理画面や他ユーザからの編集は止められない。検証中に管理画面で
  検証用ゾーンを編集しない。

## 6. クライアント証明書検証の確認

集約層を経由せず、Service のポートへ直接接続する。

```bash
kubectl run probe --rm -it --image=curlimages/curl --restart=Never -- \
  curl -sk https://<service>.<namespace>.svc:443/apis/acme.dns-platform.jp/v1alpha1
```

**期待**: TLS ハンドシェイクの段階で失敗し、要求が処理されない (SC-010)。
成功してしまう場合は、委譲認証が有効になっていない。

## 7. 証明書取得の通し確認

### ClusterIssuer (SC-001)

```bash
kubectl -n cert-manager create secret generic dpf-token --from-literal=token=...
kubectl apply -f - <<'YAML'
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: dpf-issuer
spec:
  acme:
    email: you@example.jp
    server: https://acme-v02.api.letsencrypt.org/directory
    privateKeySecretRef:
      name: issuer-private
    solvers:
      - dns01:
          webhook:
            groupName: acme.dns-platform.jp
            solverName: iij-dpf
            config:
              domainFilters: ["example.jp", "*.example.jp"]
YAML
```

Certificate を作り、`kubectl describe certificate` で Ready になることを確認する。

**期待**:

- チャレンジ中に対象ゾーンへ TXT レコードが作られる。**後片付けでは削除されない**
- **使用済みの RDATA とラベルは期限まで残る。**期限を過ぎてから同一の名前を操作すると
  回収され、0 件になる (SC-023 / SC-024)
- DPF のゾーン変更履歴の Description が `by cert-manager(<ロックオーナー>)` になっている (SC-012)

### namespace 限定 Issuer (SC-008)

`team-a` と `team-b` にそれぞれ Secret と Issuer を置き、各 namespace の Certificate が
それぞれのトークンで処理されることを確認する。

**期待**: `team-a` の Issuer の config に `tokenSecretRef.namespace: team-b` を指定した場合、
要求が失敗し `team-b` の Secret が読まれない。

### `*.example.jp` が頂点を含まないこと

`domainFilters: ["*.example.jp"]` のみを指定し、`example.jp` を対象とする Certificate を作る。

**期待**: 一致せず失敗する。頂点も許可するには `example.jp` の併記が必要。

## 8. 複数クラスタからの同時取得 (SC-021)

2 つのクラスタに、**異なるロックオーナー**で本サービスを配備する。
同一の ACME アカウント (共有した `privateKeySecretRef`) と同一の名前の Certificate を
両クラスタで同時に作る。

**期待**:

- チャレンジレコードのラベルは **1 件**である (チャレンジコードが同一のため)
- 一方の証明書取得が完了して後片付けを行っても、チャレンジ用 RDATA は残る
- 両方の証明書が取得できる
- **後片付けでは何も消えない。**期限を過ぎたのち、次に同一の名前を操作したときに
  回収され、レコードごと消える

**既知の限界**: 同一クラスタ・同一 namespace・同一 DNSName の 2 つのチャレンジが同時に
走った場合は記録が 1 件に集約される。一方が自己確認に失敗しうるが、cert-manager の
再試行で回復する。恒久的な失敗にはならない (research.md §1.2)。

## 9. 失敗時の切り分け (SC-009)

次の 5 つをそれぞれ起こし、Pod のログのみで原因が判別できることを確認する。

| 与える状態 | ログから読み取れること |
|---|---|
| 無効なトークン | DPF のエラー応答が**全文**。問い合わせ用の識別子が失われていない |
| Secret が存在しない | 恒久的な失敗として分類されたこと |
| 指定した key が存在しない | 同上 |
| 委任されたゾーンが DPF に存在しない | `ResolvedZone` と対象 FQDN、ゾーンが見つからない旨 |
| DPF へ到達できない | 一時的な失敗として分類されたこと |

**期待**: いずれのログにもトークンの値が現れない。
メトリクスのラベルとトレースの属性にゾーン名・レコード名・レコード値が既定で含まれない。
