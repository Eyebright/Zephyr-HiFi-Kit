// Package usbfs moves audio to a USB DAC through the kernel's usbfs device file.
//
// It exists because an unrooted Android app has no isochronous path in Java:
// UsbRequest documents bulk and interrupt endpoints only, and bulkTransfer
// rejects isochronous endpoints, yet the file descriptor that
// UsbDeviceConnection.getFileDescriptor() returns is an ordinary /dev/bus/usb
// handle, and usbfs submits isochronous URBs through USBDEVFS_SUBMITURB. So
// Kotlin performs the parts the Java API can do - permission, interface claim,
// SET_INTERFACE and control transfers - and this package owns the data pump.
//
// The structures and ioctl numbers mirror <linux/usbdevice_fs.h>. They are
// derived from the sizes Go actually lays out, because arm64 and armv7 disagree
// about the width of a pointer and the kernel matches the full ioctl number.
package usbfs

import (
	"errors"
	"fmt"
	"time"
	"unsafe"
)

// The one URB type and the two flags this package needs, from
// <linux/usbdevice_fs.h>.
const (
	TypeISO = 0

	flagISOAsap = 0x02 // schedule as soon as possible

	dirIn = 0x80 // the direction bit of an endpoint address
)

// maxPacketsPerURB and maxPacketBytes are the limits proc_do_submiturb()
// applies to an isochronous URB.
const (
	MaxPacketsPerURB = 128
	MaxPacketBytes   = 98304
)

// PacketDesc is usbdevfs_iso_packet_desc: one frame of an isochronous URB. The
// kernel fills in ActualLength and Status on completion.
type PacketDesc struct {
	Length       uint32
	ActualLength uint32
	Status       uint32
}

// Urb is usbdevfs_urb. Field order and types match the C struct so Go inserts
// the same padding; the ISO packet descriptors follow it inline, exactly where
// proc_submiturb() looks for them.
type Urb struct {
	Type            byte
	Endpoint        byte
	Status          int32
	Flags           uint32
	Buffer          *byte
	BufferLength    int32
	ActualLength    int32
	StartFrame      int32
	NumberOfPackets int32
	ErrorCount      int32
	Signr           uint32
	UserContext     *byte
}

// _IOC field layout for the arm and arm64 ABI: number 0..7, type 8..15, size
// 16..29, direction 30..31.
const (
	iocWrite = uintptr(1)
	iocRead  = uintptr(2)
)

func ioc(dir uintptr, nr, size uintptr) uintptr {
	return dir<<30 | uintptr('U')<<8 | nr | size<<16
}

// ioctl numbers for the calls this package makes. usbdev_do_ioctl() switches on
// the whole command word, so the direction and size bits have to match the
// kernel's own struct, which is why they come from the sizes computed above.
var (
	// SubmitUrb is USBDEVFS_SUBMITURB, an _IOR of the whole struct.
	SubmitUrb = ioc(iocRead, 10, unsafe.Sizeof(Urb{}))
	// DiscardUrb takes the URB pointer itself, and is a bare _IO.
	DiscardUrb = ioc(0, 11, 0)
	// ReapUrbNoBlock hands back the pointer of a finished URB, and answers
	// EAGAIN when the queue is empty.
	ReapUrbNoBlock = ioc(iocWrite, 13, unsafe.Sizeof(uintptr(0)))
	Capabilities   = ioc(iocRead, 26, unsafe.Sizeof(uint32(0)))
	GetSpeed       = ioc(0, 31, 0)
)

// SizeofUrb is the offset at which the inline packet descriptors begin.
const SizeofUrb = unsafe.Sizeof(Urb{})

// SizeofPacket is the stride of the inline descriptor array.
const SizeofPacket = unsafe.Sizeof(PacketDesc{})

// Source fills one transfer's payload. It returns io.EOF when the stream is
// finished; anything else stops the pump.
type Source func(dst []byte) (int, error)

// Stats counts what the pump moved, which is what the self test reports.
type Stats struct {
	Submitted  int `json:"submitted"`
	Completed  int `json:"completed"`
	Bytes      int `json:"bytes"`
	Failures   int `json:"failures"`
	ShortSlots int `json:"shortSlots"`
	Frames     int `json:"frames"`
}

// Duration reports how long the pump moved audio, in frames of the bus clock.
func (s Stats) Duration(g Geometry) time.Duration {
	return time.Duration(s.Frames) * time.Second / time.Duration(g.FramesPerSecond)
}

// ErrNotSupported reports that this kernel or build cannot run the request.
var ErrNotSupported = errors.New("usbfs: not supported")

// Geometry is the pacing model of one negotiated USB audio format.
type Geometry struct {
	Rate     int
	Channels int
	Subslot  int
	// FramesPerSecond is 1000 on a full-speed bus and 8000 on high or
	// super speed, where each frame is a 125 µs microframe.
	FramesPerSecond int
}

// SampleBytes is the size of one sample frame: every channel's subslot.
func (g Geometry) SampleBytes() int { return g.Channels * g.Subslot }

// BytesPerSecond is the sustained rate the endpoint must be fed.
func (g Geometry) BytesPerSecond() int { return g.Rate * g.SampleBytes() }

// PeakPacketBytes is the largest packet the schedule produces: the endpoint has
// to carry it, and it bounds the buffer of one URB.
func (g Geometry) PeakPacketBytes() int {
	samplesPerFrame := (g.Rate + g.FramesPerSecond - 1) / g.FramesPerSecond
	return samplesPerFrame * g.SampleBytes()
}

// Validate reports whether the geometry can be pumped at all: whole samples per
// packet and a packet the endpoint can carry.
func (g Geometry) Validate() error {
	switch {
	case g.Rate <= 0:
		return fmt.Errorf("usbfs: rate %d", g.Rate)
	case g.Channels <= 0 || g.Subslot <= 0:
		return fmt.Errorf("usbfs: sample size %d ch x %d B", g.Channels, g.Subslot)
	case g.FramesPerSecond != 1000 && g.FramesPerSecond != 8000:
		return fmt.Errorf("usbfs: %d frames per second", g.FramesPerSecond)
	}
	if peak := g.PeakPacketBytes(); peak > MaxPacketBytes {
		return fmt.Errorf("usbfs: %d B peak packet exceeds the %d B limit", peak, MaxPacketBytes)
	}
	return nil
}

// through returns the whole samples delivered by the end of the given frame, in
// exact integer maths: the schedule never accumulates a rounding error, and a
// frame that carries a fraction still delivers whole samples.
func (g Geometry) through(frame int) int {
	return int(uint64(g.Rate) * uint64(frame+1) / uint64(g.FramesPerSecond))
}

// PacketSizes returns the byte size of each of the first n frames.
func (g Geometry) PacketSizes(n int) []int {
	return g.WindowSizes(0, n)
}

// WindowSizes returns the size of the n frames that start at frame index from.
// 44.1 kHz on a high-speed bus is 5.5125 samples per microframe, so the sizes
// alternate 6, 5, 6, 5, 5 instead of drifting, and the pattern is keyed to the
// absolute frame so a resubmitted URB continues the schedule.
func (g Geometry) WindowSizes(from, n int) []int {
	out := make([]int, n)
	prev := 0
	if from > 0 {
		prev = g.through(from - 1)
	}
	for i := 0; i < n; i++ {
		cur := g.through(from + i)
		out[i] = (cur - prev) * g.SampleBytes()
		prev = cur
	}
	return out
}

// PacketsPerURB is how many frames one URB should cover: a millisecond, clamped
// to what the kernel allows.
func (g Geometry) PacketsPerURB() int {
	n := g.FramesPerSecond / 1000
	if n < 1 {
		n = 1
	}
	if n > MaxPacketsPerURB {
		n = MaxPacketsPerURB
	}
	return n
}

// URBBytes is the payload size of one URB of n packets starting at frame index
// from, plus the exact per-packet sizes.
func (g Geometry) URBBytes(from, n int) (total int, sizes []int) {
	sizes = make([]int, n)
	total = g.sizesInto(sizes, from)
	return
}

// URBPeakBytes bounds URBBytes over every phase of the schedule. It is what one
// slot of the replay ring must hold: when the sample rate does not divide the
// frame grid, consecutive URBs of the same packet count differ by a sample.
func (g Geometry) URBPeakBytes(n int) int {
	peak := 0
	sizes := make([]int, n)
	for phase := 0; phase < g.period(); phase++ {
		if total := g.sizesInto(sizes, phase); total > peak {
			peak = total
		}
	}
	return peak
}

// sizesInto writes the sizes of the frames starting at from into dst and returns
// their sum. It is the allocation-free form the pump uses when it retimes a
// recycled transfer.
func (g Geometry) sizesInto(dst []int, from int) (total int) {
	prev := 0
	if from > 0 {
		prev = g.through(from - 1)
	}
	for i := range dst {
		cur := g.through(from + i)
		dst[i] = (cur - prev) * g.SampleBytes()
		prev = cur
		total += dst[i]
	}
	return total
}

// period is how many frames pass before the schedule repeats itself. The window
// total of n packets is periodic with it, so scanning one period finds the peak.
func (g Geometry) period() int {
	fps := g.FramesPerSecond
	if fps <= 0 || g.Rate <= 0 {
		return 1
	}
	return fps / gcd(g.Rate, fps)
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		return -a
	}
	return a
}

// DepthFor returns how many URBs to keep in flight to cover latency ms.
func (g Geometry) DepthFor(latencyMS int) int {
	per := 1000 * g.PacketsPerURB() / g.FramesPerSecond
	d := latencyMS / per
	if d < 2 {
		d = 2
	}
	return d
}
