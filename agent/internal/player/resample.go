package player

import "encoding/binary"

// resampler converts interleaved little-endian PCM between sample rates with
// linear interpolation, carrying a fractional read position across calls so a
// continuous stream stays seamless. It lets the AAudio output run at the
// device's native clock while the source keeps its own rate, avoiding the USB
// re-enumeration that drops streams when the hardware clock is forced to a new
// rate. The DAC's own high-quality stage follows this lightweight one.
type resampler struct {
	channels int
	bytesPer int
	step     float64 // input frames advanced per output frame
	pos      float64 // fractional read position within the pending input
	pending  []byte  // input frames not yet consumed
}

func newResampler(srcRate, dstRate, channels, bytesPer int) *resampler {
	return &resampler{
		channels: channels,
		bytesPer: bytesPer,
		step:     float64(srcRate) / float64(dstRate),
	}
}

func (r *resampler) readSample(frame []byte, ch int) int32 {
	off := ch * r.bytesPer
	switch r.bytesPer {
	case 2:
		return int32(int16(binary.LittleEndian.Uint16(frame[off:])))
	case 3:
		return int32(int32(frame[off]) | int32(frame[off+1])<<8 | int32(int8(frame[off+2]))<<16)
	default:
		return int32(binary.LittleEndian.Uint32(frame[off:]))
	}
}

func (r *resampler) writeSample(out []byte, ch int, v int32) {
	off := ch * r.bytesPer
	switch r.bytesPer {
	case 2:
		binary.LittleEndian.PutUint16(out[off:], uint16(int16(v)))
	case 3:
		out[off] = byte(v)
		out[off+1] = byte(v >> 8)
		out[off+2] = byte(v >> 16)
	default:
		binary.LittleEndian.PutUint32(out[off:], uint32(v))
	}
}

func clampSample(v int32, bytesPer int) int32 {
	switch bytesPer {
	case 2:
		if v > 32767 {
			return 32767
		}
		if v < -32768 {
			return -32768
		}
	case 3:
		if v > 8388607 {
			return 8388607
		}
		if v < -8388608 {
			return -8388608
		}
	}
	return v
}

// process resamples one chunk of input PCM, returning whole output frames. The
// fractional position and any unconsumed input carry over to the next call, so
// streaming is continuous across chunk boundaries.
func (r *resampler) process(in []byte) []byte {
	fb := r.channels * r.bytesPer
	if fb <= 0 {
		return nil
	}
	r.pending = append(r.pending, in...)
	inFrames := len(r.pending) / fb
	span := float64(inFrames-1) - r.pos // last usable interpolation position
	if span < 0 {
		return nil
	}
	outFrames := int(span/r.step) + 1
	out := make([]byte, outFrames*fb)
	for i := 0; i < outFrames; i++ {
		p := r.pos + float64(i)*r.step
		idx := int(p)
		frac := p - float64(idx)
		a := r.pending[idx*fb : (idx+1)*fb]
		b := a
		if idx+1 < inFrames {
			b = r.pending[(idx+1)*fb : (idx+2)*fb]
		}
		for ch := 0; ch < r.channels; ch++ {
			av := r.readSample(a, ch)
			bv := r.readSample(b, ch)
			v := int32(float64(av) + (float64(bv)-float64(av))*frac)
			r.writeSample(out[i*fb:], ch, clampSample(v, r.bytesPer))
		}
	}
	// Advance the read head past the last anchor used, keeping that anchor frame
	// (and any tail) so the next call interpolates continuously. The new position
	// is the next output sample relative to the retained anchor.
	pLast := r.pos + float64(outFrames-1)*r.step
	consumed := int(pLast)
	if consumed > 0 {
		r.pending = append([]byte(nil), r.pending[consumed*fb:]...)
	}
	r.pos = pLast + r.step - float64(consumed)
	return out
}
