package commands

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"hifihub/agent/internal/serverclient"
)

// Probe is one network diagnostic result.
type Probe struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	MS     int64  `json:"ms"`
}

// NetdiagData is the netdiag command payload.
type NetdiagData struct {
	At      int64   `json:"at"`
	Results []Probe `json:"results"`
}

// ShapeNetdiag assembles the netdiag payload from probes at a timestamp. It is
// a pure function so result shaping can be unit-tested without network I/O.
func ShapeNetdiag(at int64, probes []Probe) NetdiagData {
	if probes == nil {
		probes = []Probe{}
	}
	return NetdiagData{At: at, Results: probes}
}

// FormatNetdiagLog renders a human-readable log of the probes. Pure function.
func FormatNetdiagLog(data NetdiagData) string {
	var b strings.Builder
	fmt.Fprintf(&b, "网络诊断 @ %d\n", data.At)
	for _, p := range data.Results {
		status := "FAIL"
		if p.OK {
			status = "OK"
		}
		fmt.Fprintf(&b, "[%s] %s (%dms): %s\n", status, p.Name, p.MS, p.Detail)
	}
	return b.String()
}

// BuildDNSQuery constructs a minimal DNS query packet (QDCOUNT=1) for name with
// the given QTYPE (12=PTR for DNS-SD service enumeration). Pure, unit-tested.
func BuildDNSQuery(name string, qtype uint16) []byte {
	buf := make([]byte, 0, 64)
	buf = append(buf, 0x12, 0x34) // transaction ID
	buf = append(buf, 0x00, 0x00) // flags: standard query
	buf = append(buf, 0x00, 0x01) // QDCOUNT
	buf = append(buf, 0x00, 0x00) // ANCOUNT
	buf = append(buf, 0x00, 0x00) // NSCOUNT
	buf = append(buf, 0x00, 0x00) // ARCOUNT
	for _, label := range strings.Split(name, ".") {
		if label == "" {
			continue
		}
		buf = append(buf, byte(len(label)))
		buf = append(buf, label...)
	}
	buf = append(buf, 0x00) // root label
	buf = append(buf, byte(qtype>>8), byte(qtype))
	buf = append(buf, 0x00, 0x01) // QCLASS IN
	return buf
}

// BuildSSDPRequest constructs an SSDP M-SEARCH discovery request. Pure function.
func BuildSSDPRequest() string {
	return "M-SEARCH * HTTP/1.1\r\n" +
		"HOST: 239.255.255.250:1900\r\n" +
		"MAN: \"ssdp:discover\"\r\n" +
		"MX: 1\r\n" +
		"ST: ssdp:all\r\n" +
		"\r\n"
}

// hostPort extracts host and port from a server URL, defaulting the port by
// scheme (http=80, https=443) when absent. Pure function.
func hostPort(serverURL string) (string, string) {
	u := serverURL
	scheme := "http"
	if i := strings.Index(u, "://"); i >= 0 {
		scheme = u[:i]
		u = u[i+3:]
	}
	if i := strings.IndexAny(u, "/?#"); i >= 0 {
		u = u[:i]
	}
	host, port, err := net.SplitHostPort(u)
	if err != nil {
		host, port = u, ""
	}
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return host, port
}

func probeDNS(ctx context.Context, host string) Probe {
	start := time.Now()
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var r net.Resolver
	addrs, err := r.LookupHost(cctx, host)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		return Probe{Name: "dns", OK: false, Detail: err.Error(), MS: ms}
	}
	if len(addrs) == 0 {
		return Probe{Name: "dns", OK: false, Detail: "no addresses", MS: ms}
	}
	return Probe{Name: "dns", OK: true, Detail: strings.Join(addrs, ", "), MS: ms}
}

func probeTCP(host, port string) Probe {
	start := time.Now()
	addr := net.JoinHostPort(host, port)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		return Probe{Name: "tcp", OK: false, Detail: err.Error(), MS: ms}
	}
	_ = conn.Close()
	return Probe{Name: "tcp", OK: true, Detail: "connected " + addr, MS: ms}
}

func probeMDNS() Probe {
	start := time.Now()
	addr := &net.UDPAddr{IP: net.ParseIP("224.0.0.251"), Port: 5353}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return Probe{Name: "mdns", OK: false, Detail: err.Error(), MS: time.Since(start).Milliseconds()}
	}
	defer conn.Close()
	if _, err := conn.Write(BuildDNSQuery("_services._dns-sd._udp.local", 12)); err != nil {
		return Probe{Name: "mdns", OK: false, Detail: err.Error(), MS: time.Since(start).Milliseconds()}
	}
	_ = conn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		return Probe{Name: "mdns", OK: false, Detail: "no reply: " + err.Error(), MS: ms}
	}
	return Probe{Name: "mdns", OK: true, Detail: fmt.Sprintf("reply %d bytes", n), MS: ms}
}

func probeSSDP() Probe {
	start := time.Now()
	addr := &net.UDPAddr{IP: net.ParseIP("239.255.255.250"), Port: 1900}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return Probe{Name: "ssdp", OK: false, Detail: err.Error(), MS: time.Since(start).Milliseconds()}
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(BuildSSDPRequest())); err != nil {
		return Probe{Name: "ssdp", OK: false, Detail: err.Error(), MS: time.Since(start).Milliseconds()}
	}
	_ = conn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
	buf := make([]byte, 2048)
	n, err := conn.Read(buf)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		return Probe{Name: "ssdp", OK: false, Detail: "no reply: " + err.Error(), MS: ms}
	}
	return Probe{Name: "ssdp", OK: true, Detail: fmt.Sprintf("reply %d bytes", n), MS: ms}
}

func probeMulticast() Probe {
	start := time.Now()
	addr := &net.UDPAddr{IP: net.ParseIP("239.255.255.250"), Port: 1900}
	conn, err := net.ListenMulticastUDP("udp", nil, addr)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		return Probe{Name: "multicast", OK: false, Detail: err.Error(), MS: ms}
	}
	_ = conn.Close()
	return Probe{Name: "multicast", OK: true, Detail: "joined 239.255.255.250:1900", MS: ms}
}

func (e *Executor) netdiag(ctx context.Context) serverclient.CommandResult {
	at := time.Now().Unix()
	host, port := hostPort(e.serverURL)
	probes := []Probe{
		probeDNS(ctx, host),
		probeTCP(host, port),
		probeMDNS(),
		probeSSDP(),
		probeMulticast(),
	}
	data := ShapeNetdiag(at, probes)
	return serverclient.CommandResult{Status: "ok", Log: FormatNetdiagLog(data), Data: data}
}
