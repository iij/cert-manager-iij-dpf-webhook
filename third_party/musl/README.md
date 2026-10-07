<!-- SPDX-License-Identifier: Apache-2.0 -->

# musl のライセンス本文

配布するバイナリは、ASLR を有効にした静的な実行ファイル (static-pie) とするため、ビルダ
(`golang:*-alpine`) の C ライブラリ musl を静的にリンクする。`COPYRIGHT` は musl のライセンス
本文 (MIT) であり、イメージの `/licenses/third-party/musl/COPYRIGHT` へ同梱する。

| 項目 | 値 |
|---|---|
| 版 | musl 1.2.6 (`Dockerfile` の `MUSL_VERSION`。ビルダの musl がこれと異なればビルドを失敗させる) |
| 取得元 | `v1.2.6` タグの `COPYRIGHT` (https://raw.githubusercontent.com/kraj/musl/v1.2.6/COPYRIGHT。公式は https://git.musl-libc.org/cgit/musl/plain/COPYRIGHT?h=v1.2.6) |
| SHA-256 | `b870108ec5e7790e9f9919064f1b9421d62d5f9b0e6c230c6adf7ea2da62e97b` |

ビルダの musl の版を上げる場合は、同じ版の `COPYRIGHT` に差し替え、上の表を更新する。
