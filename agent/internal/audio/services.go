package audio

import (
	"sort"
	"strings"
)

// ServiceProc is one live process attributed to an audio service app id.
type ServiceProc struct {
	PID  int
	Comm string
}

// ServiceProcesses scans /proc once and maps every live audio-protocol process
// to its service app id, so pre-existing services light up even when started
// outside systemd. A bare MPD process maps to "mpd" (moode's main player), never
// to upmpdcli: the UPnP/DLNA renderer runs its own upmpdcli process.
func ServiceProcesses(pr ProcReader) map[string][]ServiceProc {
	pids, err := pr.PIDs()
	if err != nil {
		return nil
	}
	sort.Ints(pids)
	out := map[string][]ServiceProc{}
	for _, pid := range pids {
		comm := pr.Comm(pid)
		app := serviceForComm(strings.ToLower(comm))
		if app == "" {
			continue
		}
		out[app] = append(out[app], ServiceProc{PID: pid, Comm: comm})
	}
	return out
}

// serviceForComm matches a lowercased process comm against the known audio
// protocol daemons. Order matters: upmpdcli contains "mpd" as a substring.
func serviceForComm(n string) string {
	switch {
	case strings.Contains(n, "shairport"):
		return "shairport-sync"
	case strings.Contains(n, "roon"), strings.Contains(n, "raat"):
		return "roonbridge"
	case strings.Contains(n, "networkaudiod"):
		return "networkaudiod"
	case strings.Contains(n, "upmpdcli"):
		return "upmpdcli"
	case n == "mpd":
		return "mpd"
	case strings.Contains(n, "spotifyd"):
		return "spotifyd"
	case strings.Contains(n, "snapclient"):
		return "snapclient"
	case strings.Contains(n, "squeezelite"):
		return "squeezelite"
	case strings.Contains(n, "gmediarenderer"):
		return "gmediarenderer"
	}
	return ""
}
