package usbfs

import (
	"testing"
	"unsafe"
)

// hostIs64Bit mirrors the only thing the layout assertions care about: pointers
// are 8 bytes on arm64 and 4 on armv7, and the kernel sizes the ioctl after the
// struct it reads.
func hostIs64Bit() bool { return unsafe.Sizeof(uintptr(0)) == 8 }

func TestStructLayout(t *testing.T) {
	if SizeofPacket != 12 {
		t.Errorf("usbdevfs_iso_packet_desc is %d bytes, want 12", SizeofPacket)
	}
	want := uintptr(56)
	if !hostIs64Bit() {
		want = 44
	}
	if SizeofUrb != want {
		t.Errorf("usbdevfs_urb is %d bytes on this ABI, want %d", SizeofUrb, want)
	}
}

func TestIoctlNumbers(t *testing.T) {
	// _IO numbers carry no direction or size bits.
	if DiscardUrb != uintptr(0)<<30|uintptr('U')<<8|11 {
		t.Errorf("DISCARDURB = %#x", DiscardUrb)
	}
	if GetSpeed != uintptr('U')<<8|31 {
		t.Errorf("GET_SPEED = %#x", GetSpeed)
	}
	if SubmitUrb != 2<<30|uintptr('U')<<8|10|SizeofUrb<<16 {
		t.Errorf("SUBMITURB = %#x, want the _IOR of a %d byte struct", SubmitUrb, SizeofUrb)
	}
	if ReapUrbNoBlock != 1<<30|uintptr('U')<<8|13|unsafe.Sizeof(uintptr(0))<<16 {
		t.Errorf("REAPURBNDELAY = %#x", ReapUrbNoBlock)
	}
	if Capabilities != 2<<30|uintptr('U')<<8|26|4<<16 {
		t.Errorf("GET_CAPABILITIES = %#x", Capabilities)
	}
}

func hs(rate, ch, sub int) Geometry {
	return Geometry{Rate: rate, Channels: ch, Subslot: sub, FramesPerSecond: 8000}
}

func fs(rate, ch, sub int) Geometry {
	return Geometry{Rate: rate, Channels: ch, Subslot: sub, FramesPerSecond: 1000}
}

func TestPacketSizesSpreadTheRemainder(t *testing.T) {
	g := hs(44100, 2, 4) // 5.5125 samples per microframe
	want := []int{40, 48, 40, 48, 40, 48, 40, 48}
	got := g.PacketSizes(8)
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("PacketSizes(8) = %v, want %v", got, want)
		}
	}
	if peak := g.PeakPacketBytes(); peak != 48 {
		t.Errorf("peak packet = %d B, want 48", peak)
	}
}

func TestWholeSamplesOnly(t *testing.T) {
	for _, g := range []Geometry{hs(44100, 2, 4), hs(88200, 2, 4), fs(44100, 2, 3), fs(176400, 2, 4)} {
		for i, s := range g.PacketSizes(g.FramesPerSecond) {
			if s%g.SampleBytes() != 0 {
				t.Fatalf("%d Hz on %d frames/s: frame %d carries %d B, not whole samples",
					g.Rate, g.FramesPerSecond, i, s)
			}
		}
	}
}

func TestScheduleIsDriftFree(t *testing.T) {
	rates := []int{44100, 48000, 88200, 96000, 176400, 192000, 352800, 384000}
	for _, rate := range rates {
		for _, fps := range []int{1000, 8000} {
			g := Geometry{Rate: rate, Channels: 2, Subslot: 4, FramesPerSecond: fps}
			total := 0
			for _, s := range g.PacketSizes(fps) {
				total += s
			}
			if want := g.BytesPerSecond(); total != want {
				t.Errorf("%d Hz on %d frames/s moved %d B in a second, want %d",
					rate, fps, total, want)
			}
		}
	}
}

// The pump retimes a recycled transfer against the absolute frame index, so a
// window must match the same frames counted from the start of the stream.
func TestWindowSizesMatchAbsoluteSchedule(t *testing.T) {
	g := hs(44100, 2, 4)
	full := g.PacketSizes(1000)
	for _, from := range []int{0, 1, 7, 8, 79, 80, 991, 992} {
		want := full[from : from+8]
		got := g.WindowSizes(from, 8)
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("window at frame %d = %v, want %v", from, got, want)
			}
		}
	}
}

func TestURBBytesAndPeak(t *testing.T) {
	cases := []struct {
		name  string
		g     Geometry
		n     int
		total int
		peak  int
	}{
		// 44.1 kHz: ten windows of eight microframes share 441 samples, so one
		// of them carries a sample more than the rest.
		{"44.1k high", hs(44100, 2, 4), 8, 352, 360},
		{"48k high", hs(48000, 2, 4), 8, 384, 384},
		{"192k high", hs(192000, 2, 4), 8, 1536, 1536},
		// On full speed one URB is one frame, so the peak is the peak packet.
		{"44.1k full", fs(44100, 2, 4), 1, 352, 360},
		{"44.1k full 4 packets", fs(44100, 2, 4), 4, 1408, 1416},
	}
	for _, c := range cases {
		total, sizes := c.g.URBBytes(0, c.n)
		if total != c.total {
			t.Errorf("%s: window total = %d B, want %d (%v)", c.name, total, c.total, sizes)
		}
		if peak := c.g.URBPeakBytes(c.n); peak != c.peak {
			t.Errorf("%s: peak window = %d B, want %d", c.name, peak, c.peak)
		}
	}
}

func TestPeakBoundsEveryPhase(t *testing.T) {
	for _, g := range []Geometry{hs(44100, 2, 4), hs(44100, 2, 3), fs(44100, 2, 4), hs(64000, 6, 4)} {
		n := g.PacketsPerURB()
		peak := g.URBPeakBytes(n)
		for from := 0; from < 4*g.period(); from++ {
			total, sizes := g.URBBytes(from, n)
			if total > peak {
				t.Fatalf("%d Hz: window at frame %d is %d B, peak said %d", g.Rate, from, total, peak)
			}
			if total != sum(sizes) {
				t.Fatalf("%d Hz: window at frame %d returned %d B but %v", g.Rate, from, total, sizes)
			}
		}
	}
}

func TestValidate(t *testing.T) {
	for _, g := range []Geometry{hs(44100, 2, 4), fs(48000, 2, 3), hs(192000, 8, 4)} {
		if err := g.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v", g, err)
		}
	}
	bad := []Geometry{
		{Rate: 0, Channels: 2, Subslot: 4, FramesPerSecond: 8000},
		{Rate: 44100, Channels: 0, Subslot: 4, FramesPerSecond: 8000},
		{Rate: 44100, Channels: 2, Subslot: 0, FramesPerSecond: 8000},
		{Rate: 44100, Channels: 2, Subslot: 4, FramesPerSecond: 4000},
		{Rate: 192000, Channels: 128, Subslot: 8, FramesPerSecond: 1000},
	}
	for _, g := range bad {
		if err := g.Validate(); err == nil {
			t.Errorf("Validate(%+v) accepted an impossible format", g)
		}
	}
}

func TestPacketsPerURBAndDepth(t *testing.T) {
	if n := hs(44100, 2, 4).PacketsPerURB(); n != 8 {
		t.Errorf("high speed packets per URB = %d, want 8", n)
	}
	if n := fs(44100, 2, 4).PacketsPerURB(); n != 1 {
		t.Errorf("full speed packets per URB = %d, want 1", n)
	}
	if d := hs(44100, 2, 4).DepthFor(20); d != 20 {
		t.Errorf("depth for 20 ms = %d, want 20", d)
	}
	if d := hs(44100, 2, 4).DepthFor(1); d != 2 {
		t.Errorf("depth for 1 ms = %d, want the two-URB minimum", d)
	}
}

func sum(xs []int) int {
	out := 0
	for _, x := range xs {
		out += x
	}
	return out
}
