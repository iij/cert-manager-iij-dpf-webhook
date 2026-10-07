<!-- SPDX-License-Identifier: Apache-2.0 -->

# cert-manager-iij-dpf-webhook

> [!IMPORTANT]
> 本プログラムは IIJ DNSプラットフォームサービスのサポート対象外です。
> バグ報告や機能追加の要望は、サポートセンターではなく GitHub の Issue へお願いします。

[cert-manager](https://cert-manager.io/) の DNS01 solver です。IIJ DNSプラットフォームサービス
(DPF) で管理しているドメインの証明書を、ACME の DNS-01 チャレンジで取得できるようにします。
Kubernetes の APIService (集約 API サーバ) として動作し、kube-apiserver の集約層から呼ばれます。

- groupName: `acme.dns-platform.jp`
- solverName: `iij-dpf`
- 動作を確認している cert-manager: **v1.21.2 以降**

## 導入

前提として、cert-manager v1.21.2 以降がクラスタに導入されている必要があります。本チャートは
cert-manager 本体を含みません。

### 1. アクセストークンの Secret を作る

ClusterIssuer から使う場合は、本サービスを導入する namespace に作ります。本チャートは Secret を
作りません。

```bash
kubectl -n cert-manager create secret generic dpf-token --from-file=token=./token.txt
```

### 2. 本サービスを導入する

`lockOwner` は必須です。ゾーンロックの保持者と、DPF の変更履歴の Description
(`by cert-manager(<lockOwner>)`) に用います。複数のクラスタが同じゾーンを更新する場合は、
クラスタを区別できる値にしてください (1〜32 文字、英数字と `.` `-` `_`)。

```bash
helm install dpf-webhook ./deploy/chart \
  --namespace cert-manager \
  --set lockOwner=cluster-a \
  --set image.tag=<バージョン>
```

APIService のサービング証明書は cert-manager で発行し、CA は cert-manager の cainjector が
APIService へ注入します。CA をイメージやチャートに含めません。

### 3. ClusterIssuer を作る

`domainFilters` に書いたゾーンだけが変更されます。**空または省略すると、いかなるゾーンも変更しません。**

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: dpf
spec:
  acme:
    email: you@example.jp
    server: https://acme-v02.api.letsencrypt.org/directory
    privateKeySecretRef:
      name: dpf-acme-account
    solvers:
      - dns01:
          webhook:
            groupName: acme.dns-platform.jp
            solverName: iij-dpf
            config:
              domainFilters: ["example.jp", "*.example.jp"]
```

### 4. namespace 限定の Issuer を使う (テナントごとのトークン)

namespace 限定の Issuer は、**その Issuer と同じ namespace の Secret だけ**を読みます。
`tokenSecretRef.namespace` に他の namespace を書くと、その要求は失敗します (読み替えません)。

本サービスが Secret を読めるよう、Issuer を置く namespace を `tokenSecrets.namespaces` に列挙して
導入します。namespace ごとに Secret の `get` のみを許す Role/RoleBinding が作られ、全 namespace へ
権限を与える ClusterRole は作りません。ClusterIssuer を使わない場合は
`tokenSecrets.releaseNamespace=false` とし、本サービスの namespace に権限を与えないでください。

```bash
helm upgrade --install dpf-webhook ./deploy/chart \
  --namespace cert-manager \
  --set lockOwner=cluster-a \
  --set 'tokenSecrets.namespaces={team-a,team-b}' \
  --set tokenSecrets.releaseNamespace=false
```

```bash
kubectl -n team-a create secret generic dpf-token --from-file=token=./team-a-token.txt
```

```yaml
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: dpf
  namespace: team-a
spec:
  acme:
    email: team-a@example.jp
    server: https://acme-v02.api.letsencrypt.org/directory
    privateKeySecretRef:
      name: dpf-acme-account
    solvers:
      - dns01:
          webhook:
            groupName: acme.dns-platform.jp
            solverName: iij-dpf
            config:
              domainFilters: ["team-a.example.jp", "*.team-a.example.jp"]
```

ClusterIssuer の `tokenSecretRef.namespace` で本サービス以外の namespace を指す場合も、その
namespace を `tokenSecrets.namespaces` に加えてください。

### solver 設定

| フィールド | 既定 | 説明 |
|---|---|---|
| `endpoint` | `https://api.dns-platform.jp/dpf/v1` | DPF API の接続先。https のみ |
| `tokenSecretRef.name` | `dpf-token` | トークンの Secret 名 |
| `tokenSecretRef.key` | `token` | Secret 内のキー |
| `tokenSecretRef.namespace` | 本サービスの namespace | ClusterIssuer のときのみ有効。namespace 限定の Issuer は自分の namespace の Secret だけを読む |
| `domainFilters` | `[]` | `*` (すべて。警告をログに出す)、`*.<ドメイン>` (配下のすべて。ドメイン自身を含まない)、`<ドメイン>` (完全一致) |

変更するのは、cert-manager が求めた委任先のゾーン (`ResolvedZone`) と一致する DPF のゾーンです。
`_acme-challenge` が CNAME で委譲されている場合は、委譲先のゾーンが `domainFilters` に一致する必要が
あります。

## 運用上の注意

- **後片付けの時点ではチャレンジの TXT を削除しません。** 使用済みの値は期限 (既定 1 時間) を
  過ぎたのち、同じ名前に対する次の操作で回収されます。複数のクラスタが同じチャレンジコードを
  使う場合に、一方の後片付けが他方の検証中の値を消さないためです。
- **同じゾーンを操作する他のプログラムも `dpf-go` v0.6.0 以降にそろえてください。** v0.5.0 以前とは
  ゾーンロックの方式が異なり、混在すると排他が成立しません。
- ゾーンロックはゾーンのラベルを 1 つ消費します (`lock.dpf-go`)。
- ゾーンロックの保持中も、管理画面や他ユーザからのレコードの編集は止められません。
- トークンの取得の失敗は、ログの `failure` に分類を記録します。Secret が無い・キーが無い・値が
  空・読む権限が無い場合は `permanent` で、Secret を直すまで成功しません (1 回の要求のなかでは繰り返し
  読みません)。Kubernetes API に到達できない場合は `temporary` で、1 回の読み出しにつき 3 回まで
  試します。Secret の作成や修正、トークンのローテーションは、再起動しなくても次の要求から使われます。
- 1 回の処理が kube-apiserver の要求のタイムアウト (60 秒) を越えることがあります。その場合も
  処理は続き、cert-manager の再試行で完了します (操作期限の既定は 5 分)。

## 障害の切り分け

証明書が発行されない場合は、Challenge の status に表示される理由と、本サービスの Pod のログを
確認してください。Challenge の status には DPF の応答の本文を載せません。ログの `uid` で
対応づけて全文を確認します。

```bash
kubectl -n cert-manager logs deploy/dpf-webhook | grep '"challenge failed"'
```

| ログに現れるもの | 原因 |
|---|---|
| `token secret not found` / `token key not found in secret` / `token secret value is empty` | トークンの Secret の名前・キー・値 (`failure` は `permanent`) |
| `DPF API error response` (`"status":401` など) | トークンが無効、または DPF が要求を拒否した (本文の全文と `request_id` が残る) |
| `delegated zone (ResolvedZone) not found in DPF` | `resolvedZone` のゾーンが DPF に無い |
| `zone is not allowed by domainFilters` | `domainFilters` がゾーンを許していない |
| `DPF API request failed` / `DPF API is unavailable` | DPF に到達できない (`failure` は `temporary`) |

DPF へ送った要求 (`DPF API request`) と変更操作 (`DNS record change`) も記録します。トークンは
記録しません。

メトリクスは `:8080/metrics` で公開します (`dpf_webhook_challenge_requests_total`、
`dpf_webhook_dns_record_changes_total`)。OTLP によるログ・メトリクス・トレースの送出は既定で
無効です。送出する場合は `--set otlp.endpoint=https://otel-collector:4318` のように送出先を与えて
ください (`otlp.protocol` は `grpc` または `http/protobuf`)。

## 開発

```bash
make check   # 整形・SPDX・静的解析・テスト
```

コミットと Pull Request の取り決めは [CONTRIBUTING.md](CONTRIBUTING.md) を参照してください。
変更の記録は [CHANGELOG.md](CHANGELOG.md) にあります。

## ライセンス

Apache License 2.0。[LICENSE](LICENSE) を参照してください。

Copyright 2026 Internet Initiative Japan Inc.
