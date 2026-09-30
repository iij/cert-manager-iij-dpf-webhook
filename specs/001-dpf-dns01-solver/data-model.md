# Data Model: DPF DNS01 Challenge Solver

**作成日**: 2026-09-18
**対象**: [spec.md](./spec.md) / 決定は [research.md](./research.md)

本サービスは永続ストレージを持たない。状態は DPF のゾーン上に置かれる。
ここでは (1) 入力として受け取るもの、(2) DPF 上に置く状態、(3) 内部で扱う値の 3 つを示す。

---

## 1. 入力

### ChallengeRequest (cert-manager から渡される)

`whapi.ChallengeRequest` (cert-manager v1.21.2)。本サービスが読むフィールドのみを示す。

| フィールド | 型 | 用途 |
|---|---|---|
| `Action` | `present` / `cleanup` | どちらの操作か |
| `Key` | string | チャレンジコード。TXT の RDATA として置く値 |
| `ResolvedFQDN` | string | **実際に書き込む FQDN。** 末尾ドット付き。CNAME 解決済み |
| `ResolvedZone` | string | **変更するゾーン。**上流が `ResolvedFQDN` から SOA を探索して求めた、実際に委任されているゾーン |
| `ResourceNamespace` | string | Issuer の namespace。ClusterIssuer ではクラスタリソース namespace |
| `DNSName` | string | 検証対象のドメイン。`example.jp` または `*.example.jp` |
| `AllowAmbientCredentials` | bool | **true = ClusterIssuer 由来、false = namespace 限定 Issuer 由来** |
| `Config` | JSON | solver 設定 (下記) |
| `UID` | string | **保持者の識別には使わない。**ラウンドトリップ単位のため。ログの相関にのみ用いる |

**読まないもの**: `Type` (dns-01 のみのため検証だけ行う)。

**`ResolvedZone` を正とする理由**: `ResolvedZone` は SOA の探索で求めた、実際に委任されて
いるゾーンである。**書いたレコードが ACME サーバから見えるのは、このゾーンに書いた場合に
限る。** DPF に `ResolvedZone` より長く一致する子ゾーンがあっても、それは委任が繋がって
いないゾーンであり、そこへ書いても公開 DNS から見えない。したがって DPF のゾーンのうち
`ResolvedZone` と**一致するもの**を選び、無ければ失敗させる (FR-013〜015 / research.md D-18)。
`utils.GetZoneFromName` は longest match であり、一致するゾーンが無いと親ゾーンを返すため、
返ったゾーンの名前が `ResolvedZone` と一致することを確かめる。

### SolverConfig (Issuer / ClusterIssuer の `dns01.webhook.config`)

スキーマは [contracts/solver-config.schema.json](./contracts/solver-config.schema.json)。

| フィールド | 型 | 既定 | 検証 |
|---|---|---|---|
| `endpoint` | string | `https://api.dns-platform.jp/dpf/v1` | 絶対 URL。スキームは `https` |
| `tokenSecretRef.name` | string | `dpf-token` | 空文字は既定として扱う |
| `tokenSecretRef.key` | string | `token` | 空文字は既定として扱う |
| `tokenSecretRef.namespace` | string | (下記) | ClusterIssuer 由来のときのみ有効 |
| `domainFilters` | string[] | `[]` | 各要素は 3 形式のいずれか |

**未知のフィールドは拒否する** (FR: 未知フィールドで要求を失敗させる)。
JSON のデコードで `DisallowUnknownFields` 相当の扱いをする。

**`tokenSecretRef.namespace` の解決**:

| `AllowAmbientCredentials` | 使う namespace |
|---|---|
| `false` | `ResourceNamespace`。`namespace` が指定され、かつ `ResourceNamespace` と異なるなら**失敗** |
| `true` | `namespace`。空なら本 Pod の namespace |

### DeploymentOptions (配備単位の設定)

環境変数またはコマンドライン引数で与える。認証情報は含まない。

| 設定 | 必須 | 既定 | 検証 |
|---|---|---|---|
| ロックオーナー | **必須** | なし | 1〜32 文字、英数字と `.` `-` `_`。未設定または不適合なら起動を中止 |

| 期限 (回収の期限) の猶予 | 任意 | **1 時間** | 正の期間。長すぎるとラベル上限に達する |
| 期限の更新閾値 | 任意 | 猶予の 1/2 (既定 30 分) | 猶予より短い期間。残りがこれ以上なら更新しない |

| ロック TTL | 任意 | 2 分 | 正の期間。`Apply` が実行中に自動で延長する |
| 排他の取得を試みる間隔 | 任意 | 5 秒 | 正の期間。`WithLockWait` に渡す。待機の上限は操作期限 |
| 操作期限 | 任意 | **5 分** | 正の期間。**60 秒 (apiserver の `RequestTimeout` 既定) より長くする。** 取り残された処理の上限を表す。既定値は T012 の実測で見直す |
| クラスタ既定 namespace | 任意 | 本 Pod の namespace | |

---

## 2. DPF 上に置く状態

配置の契約は [contracts/dpf-records.md](./contracts/dpf-records.md)。

### チャレンジレコード

| 属性 | 値 |
|---|---|
| 名前 | `ResolvedFQDN` の正規化名 |
| 種別 | `TXT` |
| RDATA | チャレンジコードの集合。**本サービス以外が置いた値を含みうる** |
| TTL | 新規作成時のみ設定。既存に追記する場合は変更しない。null は「ゾーン既定を継承」を意味し、0 は不正 (`minimum: 1`) |

### 期限の記録 (チャレンジレコードのラベル)

記録は**チャレンジレコード自身のラベル**に置く。別のレコードは作らない。
ラベルは DPF のレコードに付く key-value のメタデータであり、**DNS には公開されない。**

**ExpireEntry** (ラベルの 1 エントリ。**チャレンジコード 1 つにつき 1 つ**):

```
expire.acme.dns-platform.jp/<対象の識別値>  =  <期限 (UTC Unixtime)>
```

| 部分 | 内容 | 長さ | API 上の制約 |
|---|---|---|---|
| キーの prefix | `expire.acme.dns-platform.jp` | 27 | DNS のドメイン名の形式、最大 253 |
| キーの name | 対象の識別値 | 16 | 英数字と `.` `-` `_`、最大 63 |
| 値 | 期限 (UTC Unixtime) | 10 | `minLength: 1`, `maxLength: 63` |

**対象の識別値**: `SHA-256(チャレンジコード)` の先頭 64 ビットの 16 進小文字。
**保持者を区別しない。** 期限による回収は誰が置いたかを問わないため。

**同一性**: キーの一致で同一とみなす。既存のキーがあれば値 (期限) のみを更新する。

**件数の上限**: ラベルは 1 レコードあたり 10 件 (`RecordLabels.maxProperties`)。
この枠は**本サービス以外が付けたラベルと共有する。** 追加によって総数が 10 を超える場合は
要求を失敗させる。追い出しはしない。

**他者のラベルの保全**: `PatchRecord` の `Labels` はマップ全体を置き換えるため、
現在のラベルを読み出し、**prefix が `expire.acme.dns-platform.jp/` のキーのみ**を加除して
書き戻す。prefix が一致しないキーは必ず維持する。

**回収 (掃除)**: ダイジェストは可逆でないため、キーからチャレンジコードを復元できない。
**レコード内の各 RDATA の値から識別値を計算し、期限切れのラベルのキーと突き合わせる。**

| 状況 | 扱い |
|---|---|
| RDATA があり、対応するラベルが期限切れ | **両方を取り除く** |
| RDATA があり、対応するラベルが期限内 | 残す |
| RDATA があるが、対応するラベルが無い | **取り除かない** (他者の値でありうる) |
| ラベルが期限切れで、対応する RDATA が無い | ラベルを取り除く |

**RDATA が空のレコードは作れない**: `RecordsRdata` の `minItems` が 1 である。
回収の結果 RDATA が 1 件も残らない場合はレコードごと削除する。

### ゾーンロック (ゾーンのラベル)

`dpf-go` (v0.6.0) の `utils.Mutex` が管理する。本サービスは直接書かない。

| ラベルキー | 値 |
|---|---|
| `lock.dpf-go` (`utils.ZoneLockLabelKey`) | `<ロックオーナー>-<奪取可能時刻 (UTC Unixtime、10 桁)>` |

- **ゾーンのラベルを 1 つ恒久的に消費する。** 解放は奪取可能時刻を現在時刻へ更新するだけで、
  ラベルは残る。利用者が同じゾーンに付けられるラベルは 9 個までになる。
- ゾーンのラベルは未反映の編集の概念を持たない。ロックの取得・延長・解放はゾーン反映を
  要さず、ゾーンに未反映の編集を残さない。
- 取得の途中でのみ、一時レコード (`_dpf-go-lock` の TXT) を追加予定の状態で作り、
  取得の完了までに取り消す。

### ゾーン反映のコメント

`PatchZoneCommit.Description` = `by cert-manager(<ロックオーナー>)`。80 オクテット以内。

---

## 3. 内部で扱う値

### Name (正規化名)

ドメイン名を文字列と混同しないための型。`dns.CanonicalName` の出力 (小文字・末尾ドット) を
保持する。**境界で一度だけ生成し、以降は正規化済みを前提とする。**

生成する境界は 3 つ: ChallengeRequest のパース、solver 設定の読み込み、DPF レスポンスの解釈。

### ZoneFilter

`domainFilters` をパースした結果。3 つの形のいずれか。

| 形 | 表現 | 判定 |
|---|---|---|
| すべて | `*` | 常に一致 |
| 配下 | `*.<Name>` | `dns.IsSubDomain(base, zone)` かつ `zone != base` |
| 完全一致 | `<Name>` | 正規化名の一致 |

上記以外はパースの時点で拒否する。判定に `strings` を用いない。

### ChangeSet (変更計画)

`internal/challenge` が返す、DPF に依存しない変更の記述。
`utils.ZoneApplier` の編集関数は、この計画を受け取ってレコードの一覧へ適用する。

| フィールド | 内容 |
|---|---|
| チャレンジ RDATA の追加 | 追加すべき値 (なければ空) |
| チャレンジ RDATA の削除 | 削除すべき値 (なければ空) |
| チャレンジレコードの削除 | RDATA が空になる場合に真 |
| ラベルの追加・更新 | キーと値 (`Present` のみ) |
| 回収する RDATA とラベル | 期限切れとして取り除く組 |

**この型があることで、FR の大半が DPF なしで単体テストできる。** 現在の状態と要求を
入力に、期待する ChangeSet を照合する形になる。

### 状態遷移

同一の (FQDN, チャレンジコード) に対する記録の数を n とする。

| 遷移 | 契機 | 効果 |
|---|---|---|
| 不在 → 存在 | 最初の `Present` | RDATA とラベルを 1 回の操作で追加 |
| 存在 → 存在 | 別の実行の `Present` (同一コード) | 残りが閾値以上なら**変更しない**。下回れば期限を更新 |
| 存在 → 存在 | `Present` の再試行 (閾値の期間内) | **変更しない。**変更 API は 0 回 |
| 存在 → 存在 | RDATA はあるがラベルが無い | **ラベルを追加する** (回収の対象にするため) |
| 存在 → 存在 | `CleanUp` | **何もしない。**期限切れの回収のみ行う |
| 存在 → 不在 | 期限経過後の `Present` / `CleanUp` | 期限切れの RDATA とラベルを回収。RDATA が空になればレコードごと削除 |
| 不在 → 不在 | 回収すべきものが無い `CleanUp` | **ゾーンへの変更を行わない。**排他も反映も不要 |
