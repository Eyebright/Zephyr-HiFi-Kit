package usbplay

import (
	"fmt"
	"io"
	"math"
	"time"

	"hifihub/agent/internal/usbfs"
)

// Transport is the device half of a run: it moves packets until the source is
// done or the caller cancels. usbfs.Handle satisfies it on Linux and Android.
type Transport interface {
	Pump(ep byte, g usbfs.Geometry, packets, depth int, src usbfs.Source, stop <-chan struct{}) (*usbfs.Stats, error)
	Abandon()
}

// Encoder writes one sample into its subslot, little-endian, with the value
// already scaled to that width. The widths are the USB Audio ones: 2, 3 and 4
// bytes, so S24_3LE keeps the sample in the low three bytes and S32_LE in four.
type Encoder func(dst []byte, v int32)

// Encoders by subslot size in bytes.
var Encoders = map[int]Encoder{
	2: func(dst []byte, v int32) {
		s := uint32(v)
		dst[0] = byte(s)
		dst[1] = byte(s >> 8)
	},
	3: func(dst []byte, v int32) {
		s := uint32(v)
		dst[0] = byte(s)
		dst[1] = byte(s >> 8)
		dst[2] = byte(s >> 16)
	},
	4: func(dst []byte, v int32) {
		s := uint32(v)
		dst[0] = byte(s)
		dst[1] = byte(s >> 8)
		dst[2] = byte(s >> 16)
		dst[3] = byte(s >> 24)
	},
}

// Tone writes one frequency per channel: the fastest way to tell whether the
// pump is on clock. A frequency counter on the DAC output has to read the tone
// back exactly, and a stream whose channels differ shows a wrong subslot or
// channel count at once.
//
// The phase comes from the running sample index rather than from the packet
// layout, so a schedule that spreads 44.1 kHz over microframes still holds
// pitch: nothing here rounds to the frame grid.
type Tone struct {
	g     usbfs.Geometry
	enc   Encoder
	scale float64 // full range of the subslot, so the width does not change the level
	amp   float64
	perCh []float64 // frequency per channel, Hz
	at    int       // whole sample frames emitted so far, the phase the tone reads
}

// NewTone builds a tone for one negotiated format. hz gives one frequency per
// channel, or exactly one frequency repeated across the channels.
func NewTone(g usbfs.Geometry, hz ...float64) (*Tone, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}
	enc, ok := Encoders[g.Subslot]
	if !ok {
		return nil, fmt.Errorf("usbplay: no encoder for a %d byte subslot", g.Subslot)
	}
	freqs := make([]float64, g.Channels)
	switch {
	case len(hz) == 1:
		for i := range freqs {
			freqs[i] = hz[0]
		}
	case len(hz) == g.Channels:
		copy(freqs, hz)
	default:
		return nil, fmt.Errorf("usbplay: %d channels but %d tones", g.Channels, len(hz))
	}
	for _, f := range freqs {
		if f <= 0 || f >= float64(g.Rate)/2 {
			return nil, fmt.Errorf("usbplay: %g Hz is outside 0..%g Hz for a %d Hz stream",
				f, float64(g.Rate)/2, g.Rate)
		}
	}
	return &Tone{
		g:     g,
		enc:   enc,
		scale: math.Pow(2, float64(8*g.Subslot-1)),
		amp:   0.25,
		perCh: freqs,
	}, nil
}

// Amplitude sets the level as a fraction of full scale, 0..1.
func (t *Tone) Amplitude(v float64) {
	switch {
	case v <= 0:
		t.amp = 0
	case v > 1:
		t.amp = 1
	default:
		t.amp = v
	}
}

// Fill writes dst's worth of tone and counts the samples. dst must hold whole
// sample frames, which the pump guarantees because it sizes every packet in
// samples; anything else would encode a fraction of a channel pair.
func (t *Tone) Fill(dst []byte) error {
	sb := t.g.SampleBytes()
	if len(dst)%sb != 0 {
		return fmt.Errorf("usbplay: %d B is not whole %d B samples", len(dst), sb)
	}
	frames := len(dst) / sb
	for frame := 0; frame < frames; frame++ {
		pos := t.at + frame
		for ch := 0; ch < t.g.Channels; ch++ {
			phase := 2 * math.Pi * t.perCh[ch] * float64(pos) / float64(t.g.Rate)
			v := t.scale * t.amp * math.Sin(phase)
			// A 32-bit slot reaches past int32 at full scale, and the encoding
			// wants the nearest value rather than a wrapped one.
			v = math.Max(math.Min(v, math.MaxInt32), math.MinInt32)
			t.enc(dst[frame*sb+ch*t.g.Subslot:], int32(v))
		}
	}
	t.at += frames
	return nil
}

// Samples is how many whole sample frames the tone has produced, which is the
// clock check the prototype makes: it must come back as rate x duration.
func (t *Tone) Samples() int { return t.at }

// Source adapts the tone to the pump's reader. dur bounds the run so the self
// test reports a length instead of playing until it is cancelled: the pump
// stops cleanly at io.EOF. A zero or negative dur is an open-ended run, which
// only Stop ends.
func (t *Tone) Source(dur time.Duration) usbfs.Source {
	limit := 0
	if dur > 0 {
		limit = int(time.Duration(t.g.Rate) * dur / time.Second)
	}
	return func(dst []byte) (int, error) {
		if limit > 0 && t.Samples() >= limit {
			return 0, io.EOF
		}
		if err := t.Fill(dst); err != nil {
			return 0, err
		}
		return len(dst), nil
	}
}
