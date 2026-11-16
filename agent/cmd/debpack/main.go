// Command debpack builds a Debian package for the terminal agent without
// requiring dpkg-deb, so packages can be produced on any build host (including
// Windows). A .deb is an ar archive of debian-binary, control.tar.gz and
// data.tar.gz; this tool writes that format directly and reproducibly.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// fixed epoch keeps builds reproducible (FR-3.11 reproducible-build spirit).
var modTime = time.Unix(1700000000, 0)

type entry struct {
	name string
	data []byte
	mode int64
}

// buildTar writes directory entries first: dpkg creates missing parents for
// regular files but not for conffiles, so a conffile's directory must be shipped
// explicitly or unpack fails with "unable to create ...dpkg-new".
func buildTar(entries []entry, dirs ...string) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, d := range dirs {
		hdr := &tar.Header{
			Name: d, Mode: 0o755, ModTime: modTime,
			Uid: 0, Gid: 0, Uname: "root", Gname: "root",
			Typeflag: tar.TypeDir, Format: tar.FormatPAX,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
	}
	for _, e := range entries {
		hdr := &tar.Header{
			Name: e.name, Mode: e.mode, Size: int64(len(e.data)),
			ModTime: modTime, Uid: 0, Gid: 0, Uname: "root", Gname: "root",
			Typeflag: tar.TypeReg, Format: tar.FormatPAX,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write(e.data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func arMember(buf *bytes.Buffer, name string, data []byte) {
	fmt.Fprintf(buf, "%-16s%-12d%-6d%-6d%-8s%-10d`\n",
		name, modTime.Unix(), 0, 0, "100644", len(data))
	buf.Write(data)
	if len(data)%2 == 1 {
		buf.WriteByte('\n')
	}
}

func readFile(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "debpack: %v\n", err)
		os.Exit(1)
	}
	return b
}

// bakeVersion substitutes the release version into install.sh's assignment only.
// The script's self-check compares VERSION against the literal "@VERSION@"
// sentinel, so a global replace would make every baked script reject itself.
func bakeVersion(script []byte, version string) []byte {
	return bytes.Replace(script, []byte("VERSION=\"@VERSION@\""), []byte("VERSION=\""+version+"\""), 1)
}

// buildDeb assembles the full .deb ar archive from its constituent parts.
// installSh must already have its @VERSION@ placeholder substituted. When audio
// is true the package is the CGO ZAT sink: it links libasound at runtime, so the
// ALSA shared library is added to Depends (the t64 alternate covers Ubuntu 24.04+).
func buildDeb(agentBin, unit, env, postinst, prerm, installSh []byte, version, arch string, audio bool) ([]byte, error) {
	dataTar, err := buildTar([]entry{
		{"./usr/bin/hifihub-agent", agentBin, 0o755},
		{"./lib/systemd/system/hifihub-agent.service", unit, 0o644},
		{"./etc/hifihub/agent.env", env, 0o644},
		{"./usr/share/hifihub-agent/install.sh", installSh, 0o755},
	}, "./etc/hifihub/", "./usr/share/hifihub-agent/")
	if err != nil {
		return nil, fmt.Errorf("data tar: %w", err)
	}

	depends := "systemd, apt, dpkg, alsa-utils"
	shortDesc := "HiFiHub terminal agent and local web console"
	longDesc := ` Local console to install, enable, disable and uninstall server-provided
 audio services (RoonBridge, NAA, AirPlay, QPlay), monitor terminal
 performance, and reboot or shutdown. Applications are distributed solely
 by the HiFiHub management server.`
	if audio {
		depends += ", libasound2 | libasound2t64"
		shortDesc = "HiFiHub terminal agent with ZAT audio playback"
		longDesc = ` Terminal agent with the ZAT audio sink: receives PCM pushed by the
 music server and renders it bit-perfect through ALSA, plus the local web
 console for service management, terminal monitoring and power control.
 Requires the CGO audio build (Linux + libasound).`
	}

	control := fmt.Sprintf(`Package: hifihub-agent
Version: %s
Architecture: %s
Maintainer: HiFiHub <hifihub@localhost>
Depends: %s
Section: sound
Priority: optional
Description: %s
%s
`, version, arch, depends, shortDesc, longDesc)

	controlTar, err := buildTar([]entry{
		{"./control", []byte(control), 0o644},
		{"./postinst", postinst, 0o755},
		{"./prerm", prerm, 0o755},
		{"./conffiles", []byte("/etc/hifihub/agent.env\n"), 0o644},
	})
	if err != nil {
		return nil, fmt.Errorf("control tar: %w", err)
	}

	var deb bytes.Buffer
	deb.WriteString("!<arch>\n")
	arMember(&deb, "debian-binary", []byte("2.0\n"))
	arMember(&deb, "control.tar.gz", controlTar)
	arMember(&deb, "data.tar.gz", dataTar)
	return deb.Bytes(), nil
}

func main() {
	bin := flag.String("bin", "", "path to the compiled hifihub-agent binary")
	arch := flag.String("arch", "amd64", "Debian architecture (amd64|arm64|armhf)")
	version := flag.String("version", "0.1.0", "package version")
	out := flag.String("out", "", "output .deb path")
	pkgdir := flag.String("pkgdir", "packaging", "directory with unit/env/postinst/prerm/install.sh")
	audio := flag.Bool("audio", false, "CGO ZAT audio build: add libasound runtime dependency")
	flag.Parse()
	if *bin == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "debpack: -bin and -out are required")
		os.Exit(2)
	}

	installSh := bakeVersion(readFile(filepath.Join(*pkgdir, "install.sh")), *version)
	deb, err := buildDeb(
		readFile(*bin),
		readFile(filepath.Join(*pkgdir, "hifihub-agent.service")),
		readFile(filepath.Join(*pkgdir, "agent.env")),
		readFile(filepath.Join(*pkgdir, "postinst")),
		readFile(filepath.Join(*pkgdir, "prerm")),
		installSh,
		*version, *arch, *audio,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "debpack: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "debpack: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, deb, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "debpack: %v\n", err)
		os.Exit(1)
	}
	// Standalone installer next to the .deb so the server can serve it from
	// /dl/install.sh; identical content for every arch of the same release.
	standalone := filepath.Join(filepath.Dir(*out), "install.sh")
	if err := os.WriteFile(standalone, installSh, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "debpack: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d bytes, arch=%s, version=%s) and %s\n", *out, len(deb), *arch, *version, standalone)
}
