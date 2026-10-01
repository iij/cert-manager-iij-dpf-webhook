<!-- SPDX-License-Identifier: Apache-2.0 -->

# cert-manager-iij-dpf-webhook

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
- 1 回の処理が kube-apiserver の要求のタイムアウト (60 秒) を越えることがあります。その場合も
  処理は続き、cert-manager の再試行で完了します (操作期限の既定は 5 分)。

## 開発

```bash
make check   # 整形・SPDX・静的解析・テスト
```

コミットと Pull Request の取り決めは [CONTRIBUTING.md](CONTRIBUTING.md) を参照してください。
変更の記録は [CHANGELOG.md](CHANGELOG.md) にあります。

## ライセンス

Apache License 2.0。[LICENSE](LICENSE) を参照してください。

Copyright 2026 Internet Initiative Japan Inc.
