package core

import "testing"

func TestSafeRemoteName(t *testing.T) {
	for _, name := range []string{"logs", "release-2026.tar.gz", "中文目录"} {
		if got, err := safeRemoteName(name); err != nil || got != name {
			t.Fatalf("safeRemoteName(%q) = %q, %v", name, got, err)
		}
	}
	for _, name := range []string{"", ".", "..", "../secret", "a/b", `a\b`} {
		if _, err := safeRemoteName(name); err == nil {
			t.Fatalf("safeRemoteName(%q) should fail", name)
		}
	}
}
