package version

import (
	"regexp"
	"testing"
)

// The version is shown in the UI, so a typo would be user-visible; keep it to
// plain semantic-version digits (no "v" prefix, no prerelease suffixes).
func TestVersionIsSemver(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(Version) {
		t.Fatalf("Version = %q, want MAJOR.MINOR.PATCH", Version)
	}
}
