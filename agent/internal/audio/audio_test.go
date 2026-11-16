package audio

import "testing"

const aplaySample = `**** List of PLAYBACK Hardware Devices ****
card 0: HDMI [HDMI ATI HDMI], device 3: HDMI 0 [HDMI 0]
  Subdevices: 1/1
  Subdevice #0: subdevice #0
card 1: DAC [USB Audio DAC], device 0: USB Audio [USB Audio]
  Subdevices: 1/1
  Subdevice #0: subdevice #0
`

func TestParseAplayL(t *testing.T) {
	devs := ParseAplayL(aplaySample)
	if len(devs) != 2 {
		t.Fatalf("expected 2 devices, got %d: %+v", len(devs), devs)
	}
	if devs[1].HW != "hw:1,0" || devs[1].Name != "DAC" {
		t.Fatalf("device[1]=%+v", devs[1])
	}
	if devs[0].HW != "hw:0,3" {
		t.Fatalf("device[0]=%+v", devs[0])
	}
}

type fakeProc struct {
	pids []int
	comm map[int]string
	fds  map[int][]string
}

func (f fakeProc) PIDs() ([]int, error)                { return f.pids, nil }
func (f fakeProc) Comm(pid int) string                 { return f.comm[pid] }
func (f fakeProc) FDTargets(pid int) ([]string, error) { return f.fds[pid], nil }

func TestDetectHolders(t *testing.T) {
	fp := fakeProc{
		pids: []int{100, 200, 300},
		comm: map[int]string{100: "shairport-sync", 200: "roonbridge", 300: "bash"},
		fds: map[int][]string{
			100: {"/dev/snd/pcmC1D0p", "/dev/null", "/dev/snd/pcmC1D0p"},
			200: {"/dev/snd/pcmC1D0p"},
			300: {"/dev/snd/controlC1", "/dev/pts/0"},
		},
	}
	h := DetectHolders(fp)
	if len(h) != 2 {
		t.Fatalf("expected 2 holders (dedup), got %d: %+v", len(h), h)
	}
	procs := map[string]bool{}
	for _, x := range h {
		procs[x.Process] = true
		if x.Device != "/dev/snd/pcmC1D0p" {
			t.Fatalf("unexpected device %q", x.Device)
		}
	}
	if !procs["shairport-sync"] || !procs["roonbridge"] {
		t.Fatalf("holders=%+v", h)
	}
}

func TestPCMPlaybackRegex(t *testing.T) {
	for _, ok := range []string{"/dev/snd/pcmC0D0p", "/dev/snd/pcmC1D3p"} {
		if !pcmPlayback.MatchString(ok) {
			t.Fatalf("should match %q", ok)
		}
	}
	for _, no := range []string{"/dev/snd/pcmC0D0c", "/dev/snd/controlC0", "/dev/null"} {
		if pcmPlayback.MatchString(no) {
			t.Fatalf("should not match %q", no)
		}
	}
}

func TestServiceProcesses(t *testing.T) {
	got := ServiceProcesses(fakeProc{
		pids: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
		comm: map[int]string{
			1: "systemd", 2: "shairport-sync", 3: "shairport-sync-", 4: "networkaudiod",
			5: "RoonBridge", 6: "RAATServer", 7: "upmpdcli", 8: "mpd",
			9: "spotifyd", 10: "snapclient", 11: "squeezelite", 12: "gmediarenderer",
		},
	})
	want := map[string]int{
		"shairport-sync": 2, "networkaudiod": 1, "roonbridge": 2, "upmpdcli": 1,
		"mpd": 1, "spotifyd": 1, "snapclient": 1, "squeezelite": 1, "gmediarenderer": 1,
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected discoveries: %v", got)
	}
	for app, n := range want {
		if len(got[app]) != n {
			t.Errorf("%s: expected %d processes, got %v", app, n, got[app])
		}
	}
	if got["shairport-sync"][0].PID != 2 || got["shairport-sync"][1].PID != 3 {
		t.Errorf("shairport pids not in order: %v", got["shairport-sync"])
	}
	if got["mpd"][0].Comm != "mpd" {
		t.Errorf("mpd comm lost: %v", got["mpd"])
	}
}

func TestServiceProcessesMPDAlone(t *testing.T) {
	// A pure-MPD terminal (no upmpdcli process) must not light the UPnP/DLNA card.
	got := ServiceProcesses(fakeProc{pids: []int{1, 2}, comm: map[int]string{1: "mpd", 2: "systemd"}})
	if _, ok := got["upmpdcli"]; ok {
		t.Fatal("bare mpd must not be attributed to upmpdcli")
	}
	if len(got) != 1 || len(got["mpd"]) != 1 {
		t.Fatalf("expected only mpd discovered, got %v", got)
	}
}
