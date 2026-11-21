package decode

import (
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"
	"os"

	"hifihub/server/internal/media"
	"hifihub/zat"
)

// DSD files are not decoded to PCM here: they are packed as DoP (DSD over PCM)
// so a DoP-capable DAC receives the original 1-bit stream. Each 24-bit PCM frame
// carries a marker byte (0x05/0xFA alternating per frame) and 16 DSD bits per
// channel.

const (
	dopMarkerA = 0x05
	dopMarkerB = 0xFA
)

// dsdSource yields 16-bit DSD fragments per channel.
type dsdSource interface {
	next() (l0, l1, r0, r1 byte, ok bool, err error)
	seekFrame(frame uint64) error
}

type dsdDecoder struct {
	f        *os.File
	src      dsdSource
	format   PCMFormat
	total    uint64
	dsdRate  int
	frame    uint64
	marker   byte
	pending  []byte
	pendingO int
}

func (d *dsdDecoder) Format() PCMFormat    { return d.format }
func (d *dsdDecoder) TotalSamples() uint64 { return d.total }
func (d *dsdDecoder) DoP() bool            { return true }
func (d *dsdDecoder) DSDRate() int         { return d.dsdRate }

// SeekSample positions the packer at a DoP frame (16 DSD samples).
func (d *dsdDecoder) SeekSample(frame uint64) error {
	if err := d.src.seekFrame(frame); err != nil {
		return err
	}
	d.frame = frame
	d.pending, d.pendingO = nil, 0
	d.marker = dopMarkerA
	if frame%2 == 1 {
		d.marker = dopMarkerB
	}
	return nil
}

func (d *dsdDecoder) Read(p []byte) (int, error) {
	n := 0
	for n+6 <= len(p) {
		if len(d.pending)-d.pendingO >= 6 {
			n += copy(p[n:], d.pending[d.pendingO:d.pendingO+6])
			d.pendingO += 6
			continue
		}
		if d.frame >= d.total {
			if n > 0 {
				return n, nil
			}
			return 0, io.EOF
		}
		l0, l1, r0, r1, ok, err := d.src.next()
		if err != nil {
			return n, err
		}
		if !ok {
			if n > 0 {
				return n, nil
			}
			return 0, io.EOF
		}
		d.pending = []byte{l0, l1, d.marker, r0, r1, d.marker}
		d.pendingO = 0
		if d.marker == dopMarkerA {
			d.marker = dopMarkerB
		} else {
			d.marker = dopMarkerA
		}
		d.frame++
	}
	if n == 0 && d.frame >= d.total {
		return 0, io.EOF
	}
	return n, nil
}

func (d *dsdDecoder) Close() error {
	if d.f != nil {
		return d.f.Close()
	}
	return nil
}

type dsfSource struct {
	f         *os.File
	dataStart int64
	blockSize int
	channels  int
	bits      int
	blocks    uint64
	blockIdx  uint64
	block     []byte
	within    int
	total     uint64
	frame     uint64
}

func (s *dsfSource) seekFrame(frame uint64) error {
	s.frame = frame
	s.blockIdx = frame * 2 / uint64(s.blockSize)
	s.within = int(frame * 2 % uint64(s.blockSize))
	s.block = nil
	return nil
}

func (s *dsfSource) load() error {
	n := s.blockSize * s.channels
	buf := make([]byte, n)
	off := s.dataStart + int64(s.blockIdx)*int64(n)
	got, err := s.f.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		return err
	}
	for i := got; i < n; i++ {
		buf[i] = 0
	}
	s.block = buf
	s.within = 0
	return nil
}

func (s *dsfSource) next() (byte, byte, byte, byte, bool, error) {
	if s.frame >= s.total {
		return 0, 0, 0, 0, false, nil
	}
	for s.block == nil || s.within+2 > s.blockSize {
		if s.block != nil {
			s.blockIdx++
		}
		if s.blockIdx >= s.blocks {
			return 0, 0, 0, 0, false, nil
		}
		if err := s.load(); err != nil {
			return 0, 0, 0, 0, false, err
		}
	}
	baseL := s.within
	baseR := s.blockSize + s.within
	l0, l1 := s.block[baseL], s.block[baseL+1]
	r0, r1 := s.block[baseR], s.block[baseR+1]
	if s.bits == 1 {
		l0, l1 = bits.Reverse8(l0), bits.Reverse8(l1)
		r0, r1 = bits.Reverse8(r0), bits.Reverse8(r1)
	}
	s.within += 2
	s.frame++
	return l0, l1, r0, r1, true, nil
}

type dffSource struct {
	f         *os.File
	dataStart int64
	dataSize  int64
	channels  int
	total     uint64
	frame     uint64
	buf       []byte
	off       int
	fileOff   int64
}

func (s *dffSource) seekFrame(frame uint64) error {
	s.frame = frame
	s.fileOff = s.dataStart + int64(frame*2*uint64(s.channels))
	s.buf, s.off = nil, 0
	return nil
}

func (s *dffSource) fill() error {
	if s.buf != nil && s.off < len(s.buf) {
		return nil
	}
	chunk := make([]byte, 64*1024)
	n, err := s.f.ReadAt(chunk, s.fileOff)
	if n > 0 {
		s.buf = chunk[:n]
		s.off = 0
		s.fileOff += int64(n)
	}
	if err != nil && err != io.EOF {
		return err
	}
	return nil
}

func (s *dffSource) next() (byte, byte, byte, byte, bool, error) {
	if s.frame >= s.total {
		return 0, 0, 0, 0, false, nil
	}
	if err := s.fill(); err != nil {
		return 0, 0, 0, 0, false, err
	}
	if len(s.buf)-s.off < 2*s.channels {
		return 0, 0, 0, 0, false, nil
	}
	l0, r0 := s.buf[s.off], s.buf[s.off+1]
	l1, r1 := s.buf[s.off+2], s.buf[s.off+3]
	s.off += 4
	s.frame++
	return l0, l1, r0, r1, true, nil
}

// openDSD opens a DSF or DFF file as DoP PCM.
func openDSD(path string, pr media.ProbeResult) (Decoder, error) {
	if pr.Channels != 2 || pr.Rate <= 0 {
		return nil, fmt.Errorf("decode: DSD DoP requires stereo (got %d channels)", pr.Channels)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	head := make([]byte, 64*1024)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	d := &dsdDecoder{f: f, dsdRate: pr.Rate, marker: dopMarkerA,
		format: PCMFormat{Format: zat.S24LE, Rate: pr.Rate / 16, Channels: 2}}
	switch pr.Container {
	case "dsf":
		if len(head) < 92 || string(head[28:32]) != "fmt " {
			f.Close()
			return nil, fmt.Errorf("decode: invalid DSF header")
		}
		samples := binary.LittleEndian.Uint64(head[64:72])
		blockSize := int(binary.LittleEndian.Uint32(head[72:76]))
		d.total = samples / 16
		if blockSize <= 0 || d.total == 0 {
			f.Close()
			return nil, fmt.Errorf("decode: invalid DSF fmt fields")
		}
		dataAt := -1
		for i := 80; i+4 <= len(head); {
			if string(head[i:i+4]) == "data" {
				dataAt = i
				break
			}
			if i+12 > len(head) {
				break
			}
			sz := int64(binary.LittleEndian.Uint64(head[i+4 : i+12]))
			if sz < 0 {
				break
			}
			i += 12 + int(sz)
		}
		if dataAt < 0 {
			f.Close()
			return nil, fmt.Errorf("decode: DSF data chunk not found")
		}
		d.src = &dsfSource{f: f, dataStart: int64(dataAt + 12), blockSize: blockSize, channels: 2,
			bits: int(binary.LittleEndian.Uint32(head[60:64])), total: d.total}
		s := d.src.(*dsfSource)
		s.blocks = (samples/8 + uint64(blockSize) - 1) / uint64(blockSize)
	case "dff":
		if len(head) < 16 || string(head[12:16]) != "DSD " {
			f.Close()
			return nil, fmt.Errorf("decode: invalid DFF header")
		}
		rate, channels, dataStart, dataSize := 0, 0, int64(-1), int64(0)
		for p := 16; p+12 <= len(head); {
			id := string(head[p : p+4])
			sz := int64(binary.BigEndian.Uint64(head[p+4 : p+12]))
			body := p + 12
			if sz <= 0 || body > len(head) {
				break
			}
			if id == "PROP" {
				end := body + int(sz)
				if end > len(head) {
					end = len(head)
				}
				q := body
				if q+4 <= end && string(head[q:q+4]) == "SND " {
					q += 4
				}
				for q+12 <= end {
					sid := string(head[q : q+4])
					ssize := int64(binary.BigEndian.Uint64(head[q+4 : q+12]))
					sbody := q + 12
					if ssize <= 0 || sbody > end {
						break
					}
					switch sid {
					case "FS  ":
						if sbody+4 <= end {
							rate = int(binary.BigEndian.Uint32(head[sbody : sbody+4]))
						}
					case "CHNL":
						if sbody+2 <= end {
							channels = int(binary.BigEndian.Uint16(head[sbody : sbody+2]))
						}
					}
					q = sbody + int(ssize)
				}
			}
			if id == "DSD " {
				dataStart, dataSize = int64(body), sz
				break
			}
			p = body + int(sz)
		}
		if dataStart < 0 || channels != 2 || rate <= 0 {
			f.Close()
			return nil, fmt.Errorf("decode: invalid DFF DSD chunk")
		}
		d.dsdRate = rate
		d.format.Rate = rate / 16
		d.total = uint64(dataSize) / 4
		d.src = &dffSource{f: f, dataStart: dataStart, dataSize: dataSize, channels: 2, total: d.total, fileOff: dataStart}
	default:
		f.Close()
		return nil, fmt.Errorf("decode: unsupported DSD container %s", pr.Container)
	}
	return d, nil
}
