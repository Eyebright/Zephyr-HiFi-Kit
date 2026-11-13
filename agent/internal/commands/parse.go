package commands

import "strings"

// Package is one upgradable OS package parsed from `apt list --upgradable`.
type Package struct {
	Name      string `json:"name"`
	Current   string `json:"current"`
	Candidate string `json:"candidate"`
}

// ParseUpgradable parses `apt list --upgradable` output into packages. Lines
// look like:
//
//	name/suite candidate arch [upgradable from: current]
//
// It ignores the "Listing..." header and any line without the upgradable
// marker, and tolerates multi-suite qualifiers (e.g. name/stable,now). This is
// a pure function so it can be unit-tested against fixtures on any platform.
func ParseUpgradable(out string) []Package {
	var pkgs []Package
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.Index(line, "[upgradable from:")
		if idx < 0 {
			continue
		}
		fields := strings.Fields(line[:idx])
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		if slash := strings.Index(name, "/"); slash >= 0 {
			name = name[:slash]
		}
		candidate := fields[1]
		rest := line[idx+len("[upgradable from:"):]
		if end := strings.Index(rest, "]"); end >= 0 {
			rest = rest[:end]
		}
		current := strings.TrimSpace(rest)
		pkgs = append(pkgs, Package{Name: name, Current: current, Candidate: candidate})
	}
	return pkgs
}
