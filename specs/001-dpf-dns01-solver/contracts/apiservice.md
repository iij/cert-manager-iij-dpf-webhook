# Contract: APIService としての外部インタフェース

**対象**: [../spec.md](../spec.md) FR-001〜FR-002 / 決定は [../research.md](../research.md) §2.1, §2.2, D-07, D-08

本サービスの外部インタフェースは cert-manager webhook provider API そのものである
(憲章 原則 I)。独自のエンドポイントを追加しない。

---

## 1. 実装する契約

`github.com/cert-manager/cert-manager/pkg/acme/webhook.Solver` を実装する。
これが本サービスが提供する契約の全体であり、これ以外の公開インタフェースを持たない。

```go
type Solver interface {
    Name() string
    Present(ch *whapi.ChallengeRequest) error
    CleanUp(ch *whapi.ChallengeRequest) error
    Initialize(kubeClientConfig *restclient.Config, stopCh <-chan struct{}) error
}
```

| メソッド | 本サービスの振る舞い |
|---|---|
| `Name()` | `iij-dpf` を返す |
| `Present(ch)` | 期限切れを回収し、チャレンジコードを RDATA として置き、期限の記録 (ラベル) を追加・更新する。望む最終状態が成立していれば何も変更しない |
| `CleanUp(ch)` | **期限切れの回収のみ。**当該チャレンジの RDATA と記録を削除しない。回収すべきものが無ければ何も変更しない ([dpf-records.md](./dpf-records.md) §3) |
| `Initialize(cfg, stopCh)` | 渡された rest.Config から Secret 読み出し用クライアントを作る。配備単位の設定を検証する |

起動は `cmd.RunWebhookServer("acme.dns-platform.jp", solver)`。

**`Initialize` での検証 (fail fast)**: ロックオーナーが未設定、33 文字以上、または英数字と `.` `-` `_` 以外を含む、
猶予・操作期限・ロック TTL のいずれかが非正、のいずれかならエラーを返して起動を中止する。

## 2. Issuer / ClusterIssuer 側の指定

| 項目 | 値 |
|---|---|
| `groupName` | `acme.dns-platform.jp` |
| `solverName` | `iij-dpf` |
| `config` | [solver-config.schema.json](./solver-config.schema.json) |

```yaml
solvers:
  - dns01:
      webhook:
        groupName: acme.dns-platform.jp
        solverName: iij-dpf
        config:
          domainFilters: ["example.jp", "*.example.jp"]
```

## 3. APIService としての登録

フレームワークは `<バージョン>.<groupName>` の API グループを提供する。
cert-manager 本体からの要求は **kube-apiserver の集約層が中継する。**
cert-manager の Pod から直接呼ばれることはない。

| 項目 | 値 |
|---|---|
| API グループ | `acme.dns-platform.jp` |
| バージョン | `v1alpha1` |
| Service ポート | 443 |
| コンテナの待ち受けポート | 443 (`--secure-port` の既定) |

APIService の `spec.caBundle` には、本サービスのサーバ証明書を検証できる CA を置く。
配布経路はチャートで用意し、README に示す。

## 4. 接続の認証

フレームワークの委譲認証 (`genericoptions.NewRecommendedOptions` の
`NewDelegatingAuthenticationOptions`) が担う。**本サービスは独自実装しない。**

| 性質 | 実現 |
|---|---|
| TLS のみで待ち受ける | `SecureServing`。平文の待ち受け口を持たない |
| クライアント証明書を検証する | `kube-system/extension-apiserver-authentication` の requestheader CA で検証 |
| CA をクラスタから取得する | 同 ConfigMap を読む。イメージやチャートに焼き込まない |
| 証明書のない接続をハンドシェイクで拒否する | 上記の帰結。要求処理に至らない |
| 検証を無効化する手段を設けない | 該当するフラグを公開しない |

**確認方法**: 集約層を経由せず Service のポートへ直接 TLS 接続し、証明書検証エラーに
なることを確かめる。[../quickstart.md](../quickstart.md) の検証 6 を参照。

## 5. exposed エンドポイント

フレームワークも secure port 上に `/healthz` と `/metrics` を持つが、これらは委譲認証の
対象であり probe とスクレイプはクライアント証明書を提示できない。このため**別ポートに
認証なしの HTTP サーバを自前で立てる。**

| 項目 | 値 |
|---|---|
| ポート | 8080 |
| 提供するパス | `/healthz`、`/readyz`、`/metrics` のみ |
| 認証 | なし |

返す情報は healthz と metrics に限る。ゾーン名・レコード名・レコード値・設定内容を
返さない。`/metrics` のラベルにゾーン名・レコード名・レコード値を既定で含めない。

**`/healthz` と `/metrics` が同一ポートであること**は NetworkPolicy で区別できない前提を
生む。probe を通す設定は同時に `/metrics` を同じ送信元へ露出させる。メトリクスに機微な値を
含めない要件 (憲章 原則 V) でこれを担保する。

## 6. 必要な権限

決定の根拠は [../research.md](../research.md) D-08。

| 用途 | 対象 | 動詞 | 範囲 |
|---|---|---|---|
| 集約層の CA 取得 | `configmaps` (`extension-apiserver-authentication`) | `get`, `list`, `watch` | `kube-system` の Role |
| 認証の委譲 | `tokenreviews` (`authentication.k8s.io`) | `create` | ClusterRole |
| 認可の委譲 | `subjectaccessreviews` (`authorization.k8s.io`) | `create` | ClusterRole |
| トークン取得 | `secrets` | **`get` のみ** | 対象 namespace の Role、または ClusterRole |

- `list` / `watch` は `extension-apiserver-authentication` ConfigMap に限る
  (委譲認証が informer を用いるため)。**Secret に対する `list` / `watch` は与えない。**
- APF は無効 (`Features.EnablePriorityAndFairness = false`) のため
  `flowcontrol.apiserver.k8s.io` の権限は不要。
- ワイルドカード (`*`) によるリソース・動詞・API グループの指定を用いない。
- 専用の ServiceAccount を使う。`default` を使わない。

## 7. 通信

| 方向 | 相手 | 必要性 |
|---|---|---|
| ingress | kube-apiserver → 443 | 要求の受信 |
| ingress | probe / スクレイプ → 8080 | 死活監視と計測 |
| egress | kube-apiserver | Secret の取得、認証・認可の委譲 |
| egress | DPF API | レコードの操作 |
| egress | DNS | 名前解決 |
| egress | OTLP 送出先 | **送出先が設定された場合のみ** |

kube-apiserver はホストネットワーク上で動作することがあり、NetworkPolicy の送信元として
表現できない場合がある。その場合も ingress を全開放しない。表現しきれない残余は
クライアント証明書の検証が担う (§4)。
