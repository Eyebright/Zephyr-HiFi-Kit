//go:build linux

package usbfs

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Capability bit names as <linux/usbdevice_fs.h> defines them, for the report
// the exclusive-output self test shows.
const (
	capZeroPacket        = 0x01
	capBulkContinuation  = 0x02
	capNoPacketSizeLimit = 0x04
	capBulkScatterGather = 0x08
	capReapAfterUnplug   = 0x10
	capMmap              = 0x20
	capDropPrivileges    = 0x40
	capConnInfoEx        = 0x80
	capSuspend           = 0x100
)

const pollIn = 0x0001 // POLLIN: usbfs raises it when a completion is queued

// pollfd is struct pollfd as poll(2) expects it: same on every ABI Go targets,
// because int and short widths do not change.
type pollfd struct {
	fd      int32
	events  int16
	revents int16
}

// Handle drives one already-opened usbfs file descriptor. It does not own the
// descriptor: Java opened it and closes it when the connection goes away, so
// Abandon here only releases the transfers still in flight.
type Handle struct {
	mu    sync.Mutex
	fd    int
	live  map[uintptr]*Transfer
	caps  uint32
	speed int
}

// Transfer is one URB: the block the kernel reads (the fixed struct with its
// inline packet descriptors right behind it, exactly where proc_submiturb()
// looks), the audio buffer it points at, and the schedule it was laid out for.
//
// The block holds Go pointers, which is what the kernel wants here: an OUT
// isochronous URB is copied at submit time, so the buffer is free to reuse as
// soon as USBDEVFS_SUBMITURB returns, and REAPURB reports completions by the
// address of this block.
type Transfer struct {
	block   []byte
	payload []byte
	addr    uintptr // block address: the key SUBMIT sent and REAP returns
	Sizes   []int   // bytes carried by each packet of the current layout
	Bytes   int     // payload bytes the current layout uses, at most len(payload)
}

// New takes the descriptor from UsbDeviceConnection.getFileDescriptor().
func New(fd int) (*Handle, error) {
	if fd < 0 {
		return nil, fmt.Errorf("usbfs: invalid descriptor %d", fd)
	}
	h := &Handle{fd: fd, live: map[uintptr]*Transfer{}, speed: -1}
	var caps uint32
	// Old kernels reject these two queries; the pump still works without them,
	// so they only enrich the report the self test shows.
	if err := h.ioctl(Capabilities, uintptr(unsafe.Pointer(&caps))); err == nil {
		h.caps = caps
	}
	if speed, err := h.ioctlValue(GetSpeed); err == nil {
		h.speed = speed
	}
	return h, nil
}

// Capabilities returns the bitmask USBDEVFS_GET_CAPABILITIES reported.
func (h *Handle) Capabilities() uint32 { return h.caps }

// CapabilityNames renders the bitmask for the UI.
func CapabilityNames(caps uint32) []string {
	table := []struct {
		bit  uint32
		name string
	}{
		{capZeroPacket, "zero-packet"},
		{capBulkContinuation, "bulk-continuation"},
		{capNoPacketSizeLimit, "no-packet-size-limit"},
		{capBulkScatterGather, "bulk-scatter-gather"},
		{capReapAfterUnplug, "reap-after-disconnect"},
		{capMmap, "mmap"},
		{capDropPrivileges, "drop-privileges"},
		{capConnInfoEx, "connect-info-ex"},
		{capSuspend, "suspend-control"},
	}
	var out []string
	for _, e := range table {
		if caps&e.bit != 0 {
			out = append(out, e.name)
		}
	}
	return out
}

// BusSpeed names the speed usbfs reported. The kernel returns its
// USB_SPEED_* enum where GET_SPEED is asked, and the pump needs it to pick the
// frame grid: full speed runs 1000 frames a second, high and super 8000.
func (h *Handle) BusSpeed() (string, bool) {
	switch h.speed {
	case 1:
		return "low", true
	case 2:
		return "full", true
	case 3:
		return "high", true
	case 4:
		return "wireless", true
	case 5:
		return "super", true
	case 6:
		return "super-plus", true
	}
	return "", false
}

// SpeedValue is the raw USB_SPEED_* number, or -1 when GET_SPEED failed.
func (h *Handle) SpeedValue() int { return h.speed }

// FramesPerSecond is the frame grid the bus speed implies.
func (h *Handle) FramesPerSecond() (int, error) {
	name, ok := h.BusSpeed()
	if !ok {
		return 0, fmt.Errorf("usbfs: bus speed %d unknown", h.speed)
	}
	if name == "full" || name == "low" {
		return 1000, nil
	}
	return 8000, nil
}

// NewTransfer allocates one slot of a replay ring: n packets of g on the OUT
// endpoint ep. The payload is sized for the largest window the schedule
// produces, because a rate that does not divide the frame grid makes otherwise
// identical URBs differ by a sample.
func (h *Handle) NewTransfer(g Geometry, ep byte, n int) (*Transfer, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}
	if n < 1 || n > MaxPacketsPerURB {
		return nil, fmt.Errorf("usbfs: %d packets per URB, kernel allows 1..%d", n, MaxPacketsPerURB)
	}
	if ep&dirIn != 0 {
		return nil, fmt.Errorf("usbfs: endpoint %#02x is an input endpoint", ep)
	}
	peak := g.URBPeakBytes(n)
	t := &Transfer{
		block:   make([]byte, int(SizeofUrb)+n*int(SizeofPacket)),
		payload: make([]byte, peak),
		Sizes:   make([]int, n),
	}
	t.addr = uintptr(unsafe.Pointer(&t.block[0]))
	u := t.header()
	u.Type = TypeISO
	u.Endpoint = ep
	u.Flags = flagISOAsap // let the host controller pick the frame, per packet
	u.NumberOfPackets = int32(n)
	u.Buffer = &t.payload[0]
	t.retime(g, 0)
	return t, nil
}

func (t *Transfer) header() *Urb { return (*Urb)(unsafe.Pointer(&t.block[0])) }

// desc returns the i-th inline packet descriptor.
func (t *Transfer) desc(i int) *PacketDesc {
	base := unsafe.Pointer(&t.block[0])
	return (*PacketDesc)(unsafe.Add(base, SizeofUrb+uintptr(i)*SizeofPacket))
}

// retime lays the transfer out for the n frames starting at frame index from
// and returns how many payload bytes that window carries.
func (t *Transfer) retime(g Geometry, from int) int {
	total := g.sizesInto(t.Sizes, from)
	for i, s := range t.Sizes {
		t.desc(i).Length = uint32(s)
	}
	u := t.header()
	u.BufferLength = int32(total)
	t.Bytes = total
	return total
}

// Payload exposes the audio buffer of the transfer so the caller can fill it.
// Only the first Bytes bytes are sent.
func (t *Transfer) Payload() []byte { return t.payload }

// Packets is how many frames one transfer of this ring covers.
func (t *Transfer) Packets() int { return len(t.Sizes) }

// Status reports the completion state after a reap: the URB error plus the
// packets that did not make it and the ones that came back short.
func (t *Transfer) Status() (urbErr error, failed, short int) {
	u := t.header()
	switch {
	case u.Status == 0:
	case u.Status == -int32(syscall.ENOENT):
		urbErr = errors.New("usbfs: transfer unlinked")
	default:
		urbErr = syscall.Errno(-u.Status)
	}
	for i := range t.Sizes {
		d := t.desc(i)
		if d.Status != 0 {
			failed++
		}
		if int(d.ActualLength) < int(d.Length) {
			short++
		}
	}
	return
}

// Delivered counts the bytes the kernel reports it handed to the device.
func (t *Transfer) Delivered() int { return int(t.header().ActualLength) }

// Submit queues the transfer for its endpoint.
func (h *Handle) Submit(t *Transfer) error {
	h.mu.Lock()
	h.live[t.addr] = t
	h.mu.Unlock()
	if err := h.ioctl(SubmitUrb, t.addr); err != nil {
		h.mu.Lock()
		delete(h.live, t.addr)
		h.mu.Unlock()
		return err
	}
	runtime.KeepAlive(t)
	return nil
}

// Reap waits for one finished transfer, giving up after the timeout.
func (h *Handle) Reap(timeout time.Duration) (*Transfer, error) {
	var ptr uintptr
	deadline := time.Now().Add(timeout)
	for {
		err := h.ioctl(ReapUrbNoBlock, uintptr(unsafe.Pointer(&ptr)))
		if err == nil {
			h.mu.Lock()
			t := h.live[ptr]
			delete(h.live, ptr)
			h.mu.Unlock()
			if t == nil {
				return nil, fmt.Errorf("usbfs: reaped an unknown transfer %#x", ptr)
			}
			return t, nil
		}
		if !errors.Is(err, syscall.EAGAIN) {
			return nil, err
		}
		left := time.Until(deadline)
		if left <= 0 {
			return nil, syscall.ETIMEDOUT
		}
		if !h.wait(left) {
			return nil, syscall.ETIMEDOUT
		}
	}
}

// Discard cancels a transfer the pump no longer wants to wait for. A transfer
// that already completed is not an error: it stays queued for Reap.
func (h *Handle) Discard(t *Transfer) error {
	err := h.ioctl(DiscardUrb, t.addr)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	return err
}

// Abandon cancels every transfer still in flight and empties the reap queue, so
// the next submit starts from a known state.
func (h *Handle) Abandon() {
	h.mu.Lock()
	list := make([]*Transfer, 0, len(h.live))
	for _, t := range h.live {
		list = append(list, t)
	}
	h.live = map[uintptr]*Transfer{}
	h.mu.Unlock()
	for _, t := range list {
		_ = h.Discard(t)
	}
	var ptr uintptr
	for {
		// EAGAIN means the queue is empty; ENODEV means the device went away
		// and there is nothing left to cancel.
		if err := h.ioctl(ReapUrbNoBlock, uintptr(unsafe.Pointer(&ptr))); err != nil {
			return
		}
		runtime.KeepAlive(ptr)
	}
}

func (h *Handle) ioctl(request, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(h.fd), request, arg)
	runtime.KeepAlive(h)
	if errno == 0 {
		return nil
	}
	return errno
}

// ioctlValue runs an argument-free ioctl that answers through the return value,
// which is how USBDEVFS_GET_SPEED reports the bus speed.
func (h *Handle) ioctlValue(request uintptr) (int, error) {
	r1, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(h.fd), request, 0)
	runtime.KeepAlive(h)
	if errno != 0 {
		return 0, errno
	}
	return int(r1), nil
}

// wait blocks until usbfs reports a queued completion or timeout passes.
// ppoll is the call to make here: arm64 has no bare poll syscall, and the
// stdlib exposes neither on Linux, so the eight-byte struct pollfd and the
// timespec are described by hand rather than pulled in through x/sys.
func (h *Handle) wait(timeout time.Duration) bool {
	ts := syscall.NsecToTimespec(int64(timeout))
	p := pollfd{fd: int32(h.fd), events: pollIn}
	for {
		r1, _, errno := syscall.Syscall6(syscall.SYS_PPOLL,
			uintptr(unsafe.Pointer(&p)), 1, uintptr(unsafe.Pointer(&ts)), 0, 0, 0)
		runtime.KeepAlive(p)
		runtime.KeepAlive(ts)
		switch {
		case errno == syscall.EINTR:
			continue
		case errno != 0:
			return false
		}
		return r1 > 0
	}
}

// Stall is what the pump reports when the endpoint stopped answering.
var ErrStall = errors.New("usbfs: no completions from the endpoint")

// Pump keeps depth transfers in flight on ep until src returns io.EOF, stop
// closes or a transfer fails. The ring starts silent so the endpoint never sees
// a gap while the source warms up.
func (h *Handle) Pump(ep byte, g Geometry, packets, depth int, src Source, stop <-chan struct{}) (*Stats, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}
	if packets < 1 || packets > MaxPacketsPerURB {
		return nil, fmt.Errorf("usbfs: %d packets per URB, kernel allows 1..%d", packets, MaxPacketsPerURB)
	}
	if depth < 2 {
		depth = 2
	}
	ring := make([]*Transfer, depth)
	for i := range ring {
		t, err := h.NewTransfer(g, ep, packets)
		if err != nil {
			h.Abandon()
			return nil, err
		}
		t.retime(g, i*packets)
		ring[i] = t
	}
	stats := &Stats{}
	for _, t := range ring {
		if err := h.Submit(t); err != nil {
			h.Abandon()
			return stats, fmt.Errorf("usbfs: submit: %w", err)
		}
		stats.Submitted++
	}
	defer h.Abandon()

	frame := depth * packets
	last := time.Now()
	for {
		select {
		case <-stop:
			return stats, nil
		default:
		}
		t, err := h.Reap(200 * time.Millisecond)
		if err != nil {
			if errors.Is(err, syscall.ETIMEDOUT) {
				if time.Since(last) > 2*time.Second {
					return stats, ErrStall
				}
				continue // give the UI a chance to ask us to stop
			}
			return stats, fmt.Errorf("usbfs: reap: %w", err)
		}
		last = time.Now()
		stats.Completed++
		urbErr, failed, short := t.Status()
		if urbErr != nil {
			if errors.Is(urbErr, syscall.ENODEV) {
				return stats, fmt.Errorf("usbfs: device gone: %w", urbErr)
			}
			stats.Failures++
		}
		stats.Failures += failed
		stats.ShortSlots += short
		stats.Bytes += t.Delivered()
		stats.Frames += t.Packets()

		total := t.retime(g, frame)
		frame += t.Packets()
		if n, err := src(t.Payload()[:total]); err != nil {
			if errors.Is(err, io.EOF) {
				return stats, nil
			}
			return stats, err
		} else if n < total {
			clear(t.Payload()[n:total]) // keep the endpoint fed with silence
		}
		if err := h.Submit(t); err != nil {
			return stats, fmt.Errorf("usbfs: resubmit: %w", err)
		}
		stats.Submitted++
	}
}
