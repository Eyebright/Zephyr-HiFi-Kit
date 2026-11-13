package commands

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestParseUpgradable(t *testing.T) {
	out := "Listing... Done\n" +
		"apt/stable 2.6.1 amd64 [upgradable from: 2.6.0]\n" +
		"base-files/stable,now 12.4+deb12u6 arm64 [upgradable from: 12.4+deb12u5]\n" +
		"linux-image-amd64/stable 6.1.99-1 amd64 [upgradable from: 6.1.94-1]\n" +
		"\n" +
		"some-random-line without marker\n"
	pkgs := ParseUpgradable(out)
	if len(pkgs) != 3 {
		t.Fatalf("expected 3 packages, got %d: %+v", len(pkgs), pkgs)
	}
	want := []Package{
		{Name: "apt", Current: "2.6.0", Candidate: "2.6.1"},
		{Name: "base-files", Current: "12.4+deb12u5", Candidate: "12.4+deb12u6"},
		{Name: "linux-image-amd64", Current: "6.1.94-1", Candidate: "6.1.99-1"},
	}
	for i, w := range want {
		if pkgs[i] != w {
			t.Fatalf("pkg[%d]=%+v want %+v", i, pkgs[i], w)
		}
	}
}

func TestParseUpgradableEmpty(t *testing.T) {
	if pkgs := ParseUpgradable("Listing... Done\n"); len(pkgs) != 0 {
		t.Fatalf("expected no packages, got %+v", pkgs)
	}
	if pkgs := ParseUpgradable(""); len(pkgs) != 0 {
		t.Fatalf("expected no packages for empty input, got %+v", pkgs)
	}
}

func TestShapeNetdiag(t *testing.T) {
	probes := []Probe{
		{Name: "dns", OK: true, Detail: "1.2.3.4", MS: 12},
		{Name: "tcp", OK: false, Detail: "timeout", MS: 5000},
	}
	data := ShapeNetdiag(1700000000, probes)
	if data.At != 1700000000 || len(data.Results) != 2 {
		t.Fatalf("shaped wrong: %+v", data)
	}
	if data.Results[0].Name != "dns" || !data.Results[0].OK || data.Results[1].OK {
		t.Fatalf("probe order/flags wrong: %+v", data.Results)
	}
	// nil probes must shape to an empty (non-nil) slice for stable JSON.
	empty := ShapeNetdiag(1, nil)
	if empty.Results == nil || len(empty.Results) != 0 {
		t.Fatalf("nil probes should become empty slice: %+v", empty.Results)
	}
}

func TestFormatNetdiagLog(t *testing.T) {
	data := ShapeNetdiag(1700000000, []Probe{
		{Name: "dns", OK: true, Detail: "1.2.3.4", MS: 12},
		{Name: "ssdp", OK: false, Detail: "no reply", MS: 1500},
	})
	log := FormatNetdiagLog(data)
	for _, want := range []string{"[OK] dns (12ms)", "[FAIL] ssdp (1500ms)", "1.2.3.4"} {
		if !strings.Contains(log, want) {
			t.Fatalf("log missing %q:\n%s", want, log)
		}
	}
}

func TestBuildDNSQuery(t *testing.T) {
	q := BuildDNSQuery("_services._dns-sd._udp.local", 12)
	if len(q) < 12 {
		t.Fatalf("query too short: %d", len(q))
	}
	// Header: QDCOUNT=1, ANCOUNT=NSCOUNT=ARCOUNT=0.
	if binary.BigEndian.Uint16(q[4:6]) != 1 {
		t.Fatalf("QDCOUNT != 1")
	}
	if binary.BigEndian.Uint16(q[6:8]) != 0 || binary.BigEndian.Uint16(q[8:10]) != 0 || binary.BigEndian.Uint16(q[10:12]) != 0 {
		t.Fatalf("answer/ns/ar counts must be 0")
	}
	// Question must encode the first label "_services" (len 9) and end with
	// QTYPE=12 (PTR), QCLASS=1 (IN).
	if q[12] != byte(len("_services")) || string(q[13:13+len("_services")]) != "_services" {
		t.Fatalf("first label wrong: %v", q[12:])
	}
	if binary.BigEndian.Uint16(q[len(q)-4:len(q)-2]) != 12 {
		t.Fatalf("QTYPE != PTR(12)")
	}
	if binary.BigEndian.Uint16(q[len(q)-2:]) != 1 {
		t.Fatalf("QCLASS != IN(1)")
	}
}

func TestBuildSSDPRequest(t *testing.T) {
	req := BuildSSDPRequest()
	for _, want := range []string{"M-SEARCH * HTTP/1.1\r\n", "ST: ssdp:all\r\n", "MAN: \"ssdp:discover\"", "HOST: 239.255.255.250:1900"} {
		if !strings.Contains(req, want) {
			t.Fatalf("ssdp request missing %q:\n%s", want, req)
		}
	}
	if !strings.HasSuffix(req, "\r\n\r\n") {
		t.Fatalf("ssdp request must end with blank line")
	}
}

func TestHostPort(t *testing.T) {
	cases := []struct {
		url, host, port string
	}{
		{"http://127.0.0.1:8080", "127.0.0.1", "8080"},
		{"http://example.com", "example.com", "80"},
		{"https://example.com/", "example.com", "443"},
		{"https://example.com:8443/path", "example.com", "8443"},
		{"http://[::1]:8080", "::1", "8080"},
	}
	for _, c := range cases {
		h, p := hostPort(c.url)
		if h != c.host || p != c.port {
			t.Fatalf("hostPort(%q)=(%q,%q) want (%q,%q)", c.url, h, p, c.host, c.port)
		}
	}
}
