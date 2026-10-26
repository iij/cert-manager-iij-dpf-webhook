// SPDX-License-Identifier: Apache-2.0

package version

import "testing"

func TestDefaults(t *testing.T) {
	if Version != "dev" || Revision != "unknown" {
		t.Fatalf("defaults = %q %q", Version, Revision)
	}
	if got := String(); got != "dev (unknown)" {
		t.Fatalf("String() = %q", got)
	}
}

func TestEmbedded(t *testing.T) {
	v, r := Version, Revision
	t.Cleanup(func() { Version, Revision = v, r })
	Version, Revision = "v0.1.0", "abc1234"
	if got := String(); got != "v0.1.0 (abc1234)" {
		t.Fatalf("String() = %q", got)
	}
}
