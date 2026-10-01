// SPDX-License-Identifier: Apache-2.0

// Package challenge は、チャレンジレコードの現在の状態と要求から、あるべき変更を導く。
//
// この層は DPF の型を参照しない (憲章 原則 II)。記録の設計は research.md D-01〜D-06、
// 配置の契約は contracts/dpf-records.md §3 を参照。
package challenge

import (
	"crypto/sha256"
	"encoding/hex"
)

// TargetID はチャレンジコードの識別値を返す。SHA-256 の先頭 64 ビットを 16 進小文字
// 16 文字で表す (D-02)。ダイジェストであり、チャレンジコードを復元できない (FR-071)。
func TargetID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}
