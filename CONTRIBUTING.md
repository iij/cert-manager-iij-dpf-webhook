<!-- SPDX-License-Identifier: Apache-2.0 -->

# Contributing

本プロジェクトの規範は [`.specify/memory/constitution.md`](.specify/memory/constitution.md)
にある。ここではコミットと Pull Request の取り決めのみを記す。

## テストファースト (憲章 原則 III)

- **テストと実装は別のコミットにし、テストのコミットを先に置く。**
- テストのコミットの時点で、そのテストが期待どおり失敗することを確認する。
  失敗の確認結果 (`go test` の出力の要約) を Pull Request の記述に残す。
- バグ修正は、まず当該バグを再現して失敗するテストのコミットから始める。
- Pull Request のテンプレートの表に、テストのコミット・失敗の確認・実装のコミットを記入する。

## 検査

```bash
make check   # 整形・SPDX・ドメイン名の判定・ACME の接続先・静的解析・テスト
make vuln    # govulncheck
```

## 変更の記録

利用者に見える挙動を変えた場合は、同じ Pull Request で `CHANGELOG.md` の `Unreleased` を
更新する (憲章「変更の記録」)。
