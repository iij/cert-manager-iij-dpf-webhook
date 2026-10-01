// SPDX-License-Identifier: Apache-2.0

package challenge

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// LabelPrefix は本サービスの記録であることを示すラベルキーの prefix (D-04)。
// prefix が一致しないラベルは他者のものとして維持する (FR-069)。
const LabelPrefix = "expire.acme.dns-platform.jp"

// MaxLabels はレコードあたりのラベルの上限 (RecordLabels.maxProperties)。
const MaxLabels = 10

// targetIDLen は識別値の文字数。
const targetIDLen = 16

// ErrInvalidExpiry は期限の値を解釈できない場合に返される。
var ErrInvalidExpiry = errors.New("challenge: invalid expiry value")

// LabelKey は識別値に対応するラベルキーを返す。
func LabelKey(id string) string {
	return LabelPrefix + "/" + id
}

// ParseLabelKey は本サービスのラベルキーから識別値を取り出す。本サービスのものでなければ
// ok は false である。ラベルキーは DNS 名ではなくメタデータの名前であり、区切りの分割に
// strings を用いる。
func ParseLabelKey(key string) (id string, ok bool) {
	prefix, name, found := strings.Cut(key, "/")
	if !found || prefix != LabelPrefix || len(name) != targetIDLen {
		return "", false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return "", false
		}
	}
	return name, true
}

// EncodeExpiry は期限を UTC Unixtime の 10 進表記にする。
func EncodeExpiry(t time.Time) string {
	return strconv.FormatInt(t.Unix(), 10)
}

// DecodeExpiry は EncodeExpiry の逆である。
func DecodeExpiry(v string) (time.Time, error) {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return time.Time{}, ErrInvalidExpiry
	}
	return time.Unix(n, 0).UTC(), nil
}

// QuoteTXT はチャレンジコードを TXT の RDATA の表記 (引用符で囲んだ文字列) にする。
func QuoteTXT(key string) string {
	return `"` + key + `"`
}

// UnquoteTXT は QuoteTXT の逆である。単一の引用符付き文字列でなければ ok は false である。
func UnquoteTXT(v string) (string, bool) {
	if len(v) < 2 || v[0] != '"' || v[len(v)-1] != '"' {
		return "", false
	}
	return v[1 : len(v)-1], true
}
