package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"strconv"
	"strings"
	"testing"
)

// readAr parses an ar archive into ordered member names and payloads.
func readAr(t *testing.T, b []byte) ([]string, map[string][]byte) {
	t.Helper()
	if string(b[:8]) != "!<arch>\n" {
		t.Fatalf("bad ar global header: %q", b[:8])
	}
	var names []string
	members := map[string][]byte{}
	off := 8
	for off < len(b) {
		if off+60 > len(b) {
			t.Fatalf("truncated ar header at %d", off)
		}
		hdr := b[off : off+60]
		if string(hdr[58:60]) != "`\n" {
			t.Fatalf("bad ar member magic at %d: %q", off, hdr[58:60])
		}
		name := strings.TrimSpace(string(hdr[0:16]))
		size, err := strconv.Atoi(strings.TrimSpace(string(hdr[48:58])))
		if err != nil {
			t.Fatalf("bad ar size for %s: %v", name, err)
		}
		off += 60
		if off+size > len(b) {
			t.Fatalf("truncated ar member %s", name)
		}
		members[name] = b[off : off+size]
		names = append(names, name)
		off += size
		if size%2 == 1 {
			off++
		}
	}
	return names, members
}

// listTar gunzips and lists tar entries with their modes and contents.
func listTar(t *testing.T, gz []byte) map[string]tar.Header {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	out := map[string]tar.Header{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		data, _ := io.ReadAll(tr)
		hdr.Size = int64(len(data))
		out[hdr.Name] = *hdr
		// stash content via a side map keyed by name using a closure trick
		tarContents[hdr.Name] = data
	}
	return out
}

var tarContents = map[string][]byte{}

func TestBakeVersion(t *testing.T) {
	script := []byte("VERSION=\"@VERSION@\"\nif [ \"$VERSION\" = \"@VERSION@\" ]; then exit 1; fi\nPKG=\"hifihub-agent_${VERSION}_${ARCH}.deb\"\n")
	got := string(bakeVersion(script, "9.9.9"))
	want := "VERSION=\"9.9.9\"\nif [ \"$VERSION\" = \"@VERSION@\" ]; then exit 1; fi\nPKG=\"hifihub-agent_${VERSION}_${ARCH}.deb\"\n"
	if got != want {
		t.Fatalf("bakeVersion:\n got %q\nwant %q", got, want)
	}
}

func TestDebRoundTrip(t *testing.T) {
	agentBin := []byte("fake-agent-binary")
	unit := []byte("[Unit]\nDescription=test\n")
	env := []byte("HIFIHUB_AGENT_SERVER_URL=http://x\n")
	postinst := []byte("#!/bin/sh\nexit 0\n")
	prerm := []byte("#!/bin/sh\nexit 0\n")
	installSh := []byte("#!/usr/bin/env bash\nVERSION=\"1.2.3\"\n")

	deb, err := buildDeb(agentBin, unit, env, postinst, prerm, installSh, "1.2.3", "arm64", false)
	if err != nil {
		t.Fatal(err)
	}
	names, members := readAr(t, deb)
	wantOrder := []string{"debian-binary", "control.tar.gz", "data.tar.gz"}
	for i, n := range wantOrder {
		if names[i] != n {
			t.Fatalf("member order[%d]=%s want %s", i, names[i], n)
		}
	}
	if string(members["debian-binary"]) != "2.0\n" {
		t.Fatalf("debian-binary=%q", members["debian-binary"])
	}

	ctrl := listTar(t, members["control.tar.gz"])
	for _, n := range []string{"./control", "./postinst", "./prerm", "./conffiles"} {
		if _, ok := ctrl[n]; !ok {
			t.Fatalf("control.tar.gz missing %s", n)
		}
	}
	if ctrl["./postinst"].Mode != 0o755 {
		t.Fatalf("postinst mode=%o want 755", ctrl["./postinst"].Mode)
	}
	control := string(tarContents["./control"])
	for _, want := range []string{"Package: hifihub-agent", "Version: 1.2.3", "Architecture: arm64", "Depends: systemd"} {
		if !strings.Contains(control, want) {
			t.Fatalf("control missing %q:\n%s", want, control)
		}
	}

	data := listTar(t, members["data.tar.gz"])
	for _, n := range []string{"./usr/bin/hifihub-agent", "./lib/systemd/system/hifihub-agent.service", "./etc/hifihub/agent.env", "./usr/share/hifihub-agent/install.sh"} {
		if _, ok := data[n]; !ok {
			t.Fatalf("data.tar.gz missing %s", n)
		}
	}
	if data["./usr/bin/hifihub-agent"].Mode != 0o755 {
		t.Fatalf("agent mode=%o want 755", data["./usr/bin/hifihub-agent"].Mode)
	}
	for _, d := range []string{"./etc/hifihub/", "./usr/share/hifihub-agent/"} {
		hdr, ok := data[d]
		if !ok {
			t.Fatalf("data.tar.gz missing directory entry %s", d)
		}
		if hdr.Typeflag != tar.TypeDir {
			t.Fatalf("%s typeflag=%d want TypeDir", d, hdr.Typeflag)
		}
		if hdr.Mode != 0o755 {
			t.Fatalf("%s mode=%o want 755", d, hdr.Mode)
		}
	}
	if data["./usr/share/hifihub-agent/install.sh"].Mode != 0o755 {
		t.Fatalf("install.sh mode=%o want 755", data["./usr/share/hifihub-agent/install.sh"].Mode)
	}
	if !bytes.Equal(tarContents["./usr/share/hifihub-agent/install.sh"], installSh) {
		t.Fatal("install.sh content mismatch")
	}
	if !bytes.Equal(tarContents["./usr/bin/hifihub-agent"], agentBin) {
		t.Fatal("agent binary content mismatch")
	}
	if !bytes.Equal(tarContents["./etc/hifihub/agent.env"], env) {
		t.Fatal("env content mismatch")
	}
}

// TestAudioDepends verifies the CGO audio variant declares the ALSA runtime
// library (with the Ubuntu 24.04 t64 alternate) while the pure-Go variant does
// not, since the stub player links no audio library.
func TestAudioDepends(t *testing.T) {
	args := []byte("bin")
	build := func(audio bool) string {
		deb, err := buildDeb(args, args, args, args, args, args, "9.9.9", "amd64", audio)
		if err != nil {
			t.Fatal(err)
		}
		_, members := readAr(t, deb)
		listTar(t, members["control.tar.gz"])
		return string(tarContents["./control"])
	}
	plain := build(false)
	if strings.Contains(plain, "libasound") {
		t.Fatalf("pure-Go control must not depend on libasound:\n%s", plain)
	}
	if !strings.Contains(plain, "Depends: systemd, apt, dpkg, alsa-utils") {
		t.Fatalf("pure-Go depends changed:\n%s", plain)
	}
	audio := build(true)
	if !strings.Contains(audio, "libasound2 | libasound2t64") {
		t.Fatalf("audio control missing ALSA runtime dependency:\n%s", audio)
	}
	if !strings.Contains(audio, "ZAT audio playback") {
		t.Fatalf("audio control missing playback description:\n%s", audio)
	}
}
