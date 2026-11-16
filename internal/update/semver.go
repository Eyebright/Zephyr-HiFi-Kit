package update

import (
	"regexp"
	"strconv"
	"strings"
)

// versionRE bounds feed tags to dotted numeric versions so a hostile feed
// cannot smuggle path segments into staging paths via the tag.
var versionRE = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,3}(-[0-9A-Za-z.]+)?$`)

// ValidVersion reports whether v is a safe dotted version string.
func ValidVersion(v string) bool { return versionRE.MatchString(v) }

// Compare orders two dotted numeric versions (v-prefix tolerated). It returns
// -1 when a < b, 0 when equal and 1 when a > b. Non-numeric suffixes compare
// lexically so pre-release tags never outrank their release.
func Compare(a, b string) int {
	pa, pb := splitVer(a), splitVer(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var xa, xb string
		if i < len(pa) {
			xa = pa[i]
		}
		if i < len(pb) {
			xb = pb[i]
		}
		na, ea := strconv.Atoi(xa)
		nb, eb := strconv.Atoi(xb)
		if ea == nil && eb == nil {
			if na != nb {
				if na < nb {
					return -1
				}
				return 1
			}
			continue
		}
		// A missing component ranks below a present numeric one (1.0 < 1.0.1).
		if xa == "" && xb != "" {
			return -1
		}
		if xb == "" && xa != "" {
			return 1
		}
		if c := strings.Compare(xa, xb); c != 0 {
			return c
		}
	}
	return 0
}

func splitVer(v string) []string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return nil
	}
	return strings.Split(v, ".")
}
