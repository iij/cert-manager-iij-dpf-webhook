// SPDX-License-Identifier: Apache-2.0

package challenge_test

import (
	"testing"
	"time"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/challenge"
)

// D-04: キーは expire.acme.dns-platform.jp/<対象の識別値>。
func TestLabelKey(t *testing.T) {
	id := challenge.TargetID("code")
	got := challenge.LabelKey(id)
	want := "expire.acme.dns-platform.jp/" + id
	if got != want {
		t.Fatalf("LabelKey = %q, want %q", got, want)
	}
	gotID, ok := challenge.ParseLabelKey(got)
	if !ok || gotID != id {
		t.Fatalf("ParseLabelKey(%q) = %q, %v", got, gotID, ok)
	}
}

// FR-069: prefix が一致しないラベルは自分の記録ではない。
func TestParseLabelKeyRejectsOthers(t *testing.T) {
	for _, k := range []string{
		"spf-managed-by",
		"expire.cm/0123456789abcdef",
		"other.example/0123456789abcdef",
		"expire.acme.dns-platform.jp",
		"lock.dpf-go",
	} {
		if _, ok := challenge.ParseLabelKey(k); ok {
			t.Errorf("ParseLabelKey(%q) = ok", k)
		}
	}
}

// 値は UTC Unixtime。
func TestExpiryRoundTrip(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	v := challenge.EncodeExpiry(at)
	if v != "1790856000" {
		t.Fatalf("EncodeExpiry = %q", v)
	}
	got, err := challenge.DecodeExpiry(v)
	if err != nil || !got.Equal(at) {
		t.Fatalf("DecodeExpiry(%q) = %v, %v", v, got, err)
	}
	for _, bad := range []string{"", "abc", "-1", "1.5"} {
		if _, err := challenge.DecodeExpiry(bad); err == nil {
			t.Errorf("DecodeExpiry(%q) succeeded", bad)
		}
	}
}

// TXT の RDATA は引用符で囲んだ形で置く。
func TestQuoteTXT(t *testing.T) {
	if got := challenge.QuoteTXT("abc"); got != `"abc"` {
		t.Fatalf("QuoteTXT = %q", got)
	}
	if got, ok := challenge.UnquoteTXT(`"abc"`); !ok || got != "abc" {
		t.Fatalf("UnquoteTXT = %q, %v", got, ok)
	}
	if _, ok := challenge.UnquoteTXT(`abc`); ok {
		t.Fatalf("UnquoteTXT accepted an unquoted value")
	}
}
