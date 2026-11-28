//go:build linux

package usbfs

import (
	"errors"
	"io"
	"syscall"
	"testing"
	"unsafe"
)

func TestNewTransferBlockLayout(t *testing.T) {
	h := &Handle{fd: -1, live: map[uintptr]*Transfer{}}
	g := hs(44100, 2, 4)
	n := g.PacketsPerURB()
	tr, err := h.NewTransfer(g, 0x01, n)
	if err != nil {
		t.Fatalf("NewTransfer: %v", err)
	}
	if got, want := len(tr.block), int(SizeofUrb)+n*int(SizeofPacket); got != want {
		t.Fatalf("block is %d bytes, want %d", got, want)
	}
	if got, want := len(tr.Payload()), g.URBPeakBytes(n); got != want {
		t.Fatalf("payload is %d bytes, want the %d byte peak window", got, want)
	}
	u := tr.header()
	if u.Type != TypeISO || u.Endpoint != 0x01 || u.Flags != flagISOAsap {
		t.Errorf("header = type %d ep %#02x flags %#x", u.Type, u.Endpoint, u.Flags)
	}
	// proc_do_submiturb() rejects an isochronous URB outside 1..128 packets.
	if u.NumberOfPackets != int32(n) {
		t.Errorf("number_of_packets = %d, want %d", u.NumberOfPackets, n)
	}
	if u.Buffer != &tr.payload[0] {
		t.Errorf("buffer does not point at the payload")
	}
	if u.BufferLength != int32(tr.Bytes) || tr.Bytes != 352 {
		t.Errorf("buffer_length = %d for a %d byte window, want 352", u.BufferLength, tr.Bytes)
	}
	for i := 0; i < n; i++ {
		if got := tr.desc(i).Length; got != uint32(g.PacketSizes(n)[i]) {
			t.Errorf("packet %d length = %d, want %d", i, got, g.PacketSizes(n)[i])
		}
	}
	if tr.addr != uintptr(unsafe.Pointer(&tr.block[0])) {
		t.Errorf("the submit key is not the address of the block")
	}
}

// A recycled transfer keeps its payload and only rewrites the schedule, so the
// window it carries can be smaller than the buffer it holds.
func TestRetimeKeepsTheBuffer(t *testing.T) {
	h := &Handle{fd: -1, live: map[uintptr]*Transfer{}}
	g := hs(44100, 2, 4)
	n := 8
	tr, err := h.NewTransfer(g, 0x01, n)
	if err != nil {
		t.Fatalf("NewTransfer: %v", err)
	}
	payload := tr.Payload()
	var big, small int
	for from := 0; from < g.period(); from++ {
		total := tr.retime(g, from)
		if total > big {
			big = total
		}
		if small == 0 || total < small {
			small = total
		}
		if &tr.Payload()[0] != &payload[0] || len(tr.Payload()) != len(payload) {
			t.Fatal("retime reallocated the buffer the kernel was told about")
		}
		if tr.Bytes != total || tr.header().BufferLength != int32(total) {
			t.Fatalf("window at frame %d: bytes %d, header %d, retimed %d",
				from, tr.Bytes, tr.header().BufferLength, total)
		}
		sum := 0
		for i := range tr.Sizes {
			sum += int(tr.desc(i).Length)
		}
		if sum != total {
			t.Fatalf("window at frame %d: descriptor lengths sum to %d, header says %d", from, sum, total)
		}
	}
	if big != g.URBPeakBytes(n) {
		t.Errorf("largest window seen = %d, peak says %d", big, g.URBPeakBytes(n))
	}
	if small >= big {
		t.Fatalf("44.1 kHz windows should differ: %d and %d", small, big)
	}
}

func TestInputEndpointRejected(t *testing.T) {
	h := &Handle{fd: -1, live: map[uintptr]*Transfer{}}
	if _, err := h.NewTransfer(hs(44100, 2, 4), 0x81, 8); err == nil {
		t.Fatal("an input endpoint was accepted for output audio")
	}
	if _, err := h.NewTransfer(hs(44100, 2, 4), 0x01, 129); err == nil {
		t.Fatal("129 packets per URB was accepted")
	}
	if _, err := h.Pump(0x01, Geometry{}, 8, 4, func([]byte) (int, error) { return 0, io.EOF }, nil); err == nil {
		t.Fatal("an impossible geometry was accepted")
	}
}

func TestTransferStatus(t *testing.T) {
	h := &Handle{fd: -1, live: map[uintptr]*Transfer{}}
	g := hs(48000, 2, 4)
	tr, err := h.NewTransfer(g, 0x01, 8)
	if err != nil {
		t.Fatalf("NewTransfer: %v", err)
	}
	// Before a reap, ActualLength is still zero, so only the error and the
	// per-packet status are meaningful; short counting belongs to completions.
	urbErr, failed, short := tr.Status()
	if urbErr != nil || failed != 0 {
		t.Fatalf("a fresh transfer reports %v, %d failed", urbErr, failed)
	}
	if short != 8 {
		t.Fatalf("a fresh transfer reports %d short frames, want all 8 unfilled", short)
	}
	u := tr.header()
	u.Status = -int32(syscall.EDOM)
	u.ActualLength = 380
	u.ErrorCount = 2
	babble := -int32(syscall.EIO) // the kernel writes a frame error as a negative errno
	for i := range tr.Sizes {
		d := tr.desc(i)
		d.ActualLength = uint32(d.Length)
		if i < 2 {
			// Any non-zero frame status must show up as a failed packet.
			d.Status = uint32(babble)
			d.ActualLength = 0
		}
	}
	urbErr, failed, short = tr.Status()
	if !errors.Is(urbErr, syscall.EDOM) {
		t.Errorf("urb error = %v, want EDOM", urbErr)
	}
	if failed != 2 {
		t.Errorf("failed packets = %d, want 2", failed)
	}
	if short != 2 {
		t.Errorf("short packets = %d, want 2", short)
	}
	if tr.Delivered() != 380 {
		t.Errorf("delivered = %d, want the 380 the kernel reported", tr.Delivered())
	}
}

func TestAbandonWithoutTransfers(t *testing.T) {
	// The fd is invalid, so every ioctl must fail without panicking: the pump
	// calls Abandon on its way out of a submit that never worked.
	h := &Handle{fd: -1, live: map[uintptr]*Transfer{}}
	h.Abandon()
	if len(h.live) != 0 {
		t.Errorf("live set is %d entries after Abandon", len(h.live))
	}
	if _, err := h.Reap(0); err == nil {
		t.Error("Reap on a closed descriptor reported a transfer")
	} else if errors.Is(err, syscall.EAGAIN) {
		t.Errorf("Reap returned %v rather than the device error", err)
	}
}
