// SPDX-License-Identifier: Apache-2.0

package challenge_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/challenge"
)

// D-02: SHA-256(チャレンジコード) の先頭 64 ビットを 16 進小文字 16 文字で表す。
func TestTargetID(t *testing.T) {
	key := "LoqXcYV8q5ONbJQxbmR7SCTNo3tiAXDfowyjxAjEuX0"
	sum := sha256.Sum256([]byte(key))
	want := hex.EncodeToString(sum[:8])
	got := challenge.TargetID(key)
	if got != want {
		t.Fatalf("TargetID = %q, want %q", got, want)
	}
	if len(got) != 16 {
		t.Fatalf("len(TargetID) = %d, want 16", len(got))
	}
}

func TestTargetIDDiffers(t *testing.T) {
	if challenge.TargetID("a") == challenge.TargetID("b") {
		t.Fatalf("different keys produced the same id")
	}
}

// FR-071: 識別値からチャレンジコードを復元できない (コードそのものを含まない)。
func TestTargetIDDoesNotContainKey(t *testing.T) {
	key := "abcdef0123456789"
	if challenge.TargetID(key) == key {
		t.Fatalf("TargetID returned the key itself")
	}
}
