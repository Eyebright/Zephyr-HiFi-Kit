package decode

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"hifihub/zat"
)

// wavDecoder streams the data chunk of a PCM WAV file. WAV samples are already
// in the target little-endian format, so decoding is a bounded copy.
type wavDecoder struct {
	f            *os.File
	format       PCMFormat
	totalSamples uint64
	remaining    int64
}

func openWAV(path string) (Decoder, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	format, dataOff, dataSize, err := parseWAV(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.Seek(dataOff, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	var total uint64
	if bpf := format.BytesPerFrame(); bpf > 0 {
		total = uint64(dataSize) / uint64(bpf)
	}
	return &wavDecoder{f: f, format: format, totalSamples: total, remaining: dataSize}, nil
}

func (d *wavDecoder) Format() PCMFormat    { return d.format }
func (d *wavDecoder) TotalSamples() uint64 { return d.totalSamples }

func (d *wavDecoder) Read(p []byte) (int, error) {
	if d.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > d.remaining {
		p = p[:int(d.remaining)]
	}
	n, err := d.f.Read(p)
	d.remaining -= int64(n)
	if err != nil && err != io.EOF {
		return n, err
	}
	if n > 0 {
		return n, nil
	}
	if d.remaining > 0 {
		return 0, io.ErrUnexpectedEOF
	}
	return 0, io.EOF
}

func (d *wavDecoder) Close() error { return d.f.Close() }

// parseWAV walks the RIFF chunks and returns the PCM format plus the data chunk's
// absolute offset and size. It supports plain PCM and WAVE_FORMAT_EXTENSIBLE.
func parseWAV(r io.ReadSeeker) (PCMFormat, int64, int64, error) {
	var hdr [12]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return PCMFormat{}, 0, 0, fmt.Errorf("wav: short header: %w", err)
	}
	if string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "WAVE" {
		return PCMFormat{}, 0, 0, errors.New("wav: not a RIFF/WAVE file")
	}
	var (
		format            PCMFormat
		haveFmt, haveData bool
		dataOff, dataSize int64
	)
	for {
		var ch [8]byte
		_, err := io.ReadFull(r, ch[:])
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return PCMFormat{}, 0, 0, err
		}
		id := string(ch[0:4])
		size := int64(binary.LittleEndian.Uint32(ch[4:8]))
		body, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return PCMFormat{}, 0, 0, err
		}
		switch id {
		case "fmt ":
			if size < 16 || size > 4096 {
				return PCMFormat{}, 0, 0, fmt.Errorf("wav: bad fmt chunk size %d", size)
			}
			buf := make([]byte, size)
			if _, err := io.ReadFull(r, buf); err != nil {
				return PCMFormat{}, 0, 0, err
			}
			af := binary.LittleEndian.Uint16(buf[0:2])
			if af == 0xFFFE && size >= 40 {
				af = binary.LittleEndian.Uint16(buf[24:26]) // SubFormat GUID
			}
			bits := int(binary.LittleEndian.Uint16(buf[14:16]))
			sf, err := wavSampleFormat(af, bits)
			if err != nil {
				return PCMFormat{}, 0, 0, err
			}
			format = PCMFormat{
				Format:   sf,
				Rate:     int(binary.LittleEndian.Uint32(buf[4:8])),
				Channels: int(binary.LittleEndian.Uint16(buf[2:4])),
			}
			haveFmt = true
		case "data":
			dataOff, dataSize, haveData = body, size, true
			if _, err := r.Seek(size, io.SeekCurrent); err != nil {
				return PCMFormat{}, 0, 0, err
			}
		default:
			if _, err := r.Seek(size, io.SeekCurrent); err != nil {
				return PCMFormat{}, 0, 0, err
			}
		}
		if size%2 == 1 {
			if _, err := r.Seek(1, io.SeekCurrent); err != nil {
				return PCMFormat{}, 0, 0, err
			}
		}
		if haveFmt && haveData {
			break
		}
	}
	if !haveFmt || !haveData {
		return PCMFormat{}, 0, 0, errors.New("wav: missing fmt or data chunk")
	}
	return format, dataOff, dataSize, nil
}

func wavSampleFormat(audioFormat uint16, bits int) (zat.Format, error) {
	if audioFormat != 1 {
		return "", fmt.Errorf("wav: unsupported audio format %d (PCM only in v0.1)", audioFormat)
	}
	switch bits {
	case 16:
		return zat.S16LE, nil
	case 24:
		return zat.S24LE, nil
	case 32:
		return zat.S32LE, nil
	default:
		return "", fmt.Errorf("wav: unsupported bit depth %d", bits)
	}
}
