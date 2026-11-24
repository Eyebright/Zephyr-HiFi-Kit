// Package dsp implements a per-device equalizer applied to interleaved PCM
// before ZAT framing. It supports a 16-band graphic EQ and a parametric EQ
// (PEQ) of up to 8 RBJ filters (peaking / low-shelf / high-shelf), plus a preamp
// and a soft limiter, designed to run in the server's PCM pump.
package dsp

import (
	"math"
	"sync"

	"hifihub/zat"
)

// BandCount is the number of graphic EQ bands.
const BandCount = 16

// MaxPEQBands caps the parametric filter count.
const MaxPEQBands = 8

// BandCenters are the graphic-EQ center frequencies in Hz.
var BandCenters = [BandCount]float64{25, 40, 63, 100, 160, 250, 400, 630, 1000, 1600, 2500, 4000, 6300, 10000, 16000, 20000}

const bandQ = 2.1

// PEQBand is one parametric filter.
type PEQBand struct {
	Type string  `json:"type"` // peaking | lowshelf | highshelf
	Freq float64 `json:"freq"`
	Gain float64 `json:"gain"`
	Q    float64 `json:"q"`
}

// Config is one device's remembered EQ setting.
type Config struct {
	Enabled bool      `json:"enabled"`
	Preset  string    `json:"preset"`
	Preamp  float64   `json:"preamp"`
	Bands   []float64 `json:"bands"`
	Mode    string    `json:"mode"` // graphic | peq
	PEQ     []PEQBand `json:"peq"`
}

// Preset is a named reference curve (graphic gains).
type Preset struct {
	ID     string    `json:"id"`
	NameZH string    `json:"name_zh"`
	NameEN string    `json:"name_en"`
	Preamp float64   `json:"preamp"`
	Bands  []float64 `json:"bands"`
}

// Presets are the built-in reference curves.
var Presets = []Preset{
	{"flat", "平直", "Flat", 0, []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
	{"bass", "低音增强", "Bass boost", -3, []float64{6, 6, 5, 4, 2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
	{"vocal", "人声", "Vocal", -2, []float64{-3, -2, -2, -1, 0, 1, 3, 4, 4, 3, 2, 1, 0, -1, -1, -2}},
	{"classical", "古典", "Classical", -2, []float64{3, 3, 2, 1, 0, 0, 0, 0, 0, 1, 2, 2, 3, 3, 2, 2}},
	{"rock", "摇滚", "Rock", -3, []float64{5, 4, 3, 2, 1, 0, -1, -1, 0, 1, 2, 3, 4, 4, 3, 3}},
	{"pop", "流行", "Pop", -2, []float64{2, 3, 3, 2, 1, 0, 1, 2, 2, 1, 1, 0, 1, 1, 2, 2}},
	{"jazz", "爵士", "Jazz", -2, []float64{3, 3, 2, 1, 0, -1, -1, 0, 1, 2, 3, 3, 2, 1, 0, -1}},
	{"country", "乡村", "Country", -1, []float64{1, 2, 2, 1, 0, 0, 1, 2, 3, 3, 2, 1, 1, 1, 2, 2}},
	{"electronic", "电子", "Electronic", -3, []float64{5, 5, 4, 2, 1, 0, -1, -1, 0, 1, 2, 1, 2, 3, 4, 4}},
	{"loudness", "等响度", "Loudness", -3, []float64{4, 4, 3, 1, 0, 0, -1, -1, 0, 0, 0, 1, 2, 3, 4, 4}},
	{"headphone", "耳机参考", "Headphone reference", -3, []float64{4, 4, 3, 1, 0, 0, 0, 1, 1, 2, 2, 3, 3, 2, 1, 1}},
}

// PresetByID returns a preset and whether it exists.
func PresetByID(id string) (Preset, bool) {
	for _, p := range Presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// DefaultConfig is a disabled, flat graphic EQ.
func DefaultConfig() Config {
	return Config{Enabled: false, Preset: "flat", Preamp: 0, Bands: make([]float64, BandCount), Mode: "graphic"}
}

// Normalize clamps a config into supported ranges and band counts.
func Normalize(cfg Config) Config {
	out := Config{Enabled: cfg.Enabled, Preset: cfg.Preset, Mode: cfg.Mode}
	out.Preamp = clamp(cfg.Preamp, -12, 6)
	out.Bands = make([]float64, BandCount)
	for i := 0; i < BandCount; i++ {
		if i < len(cfg.Bands) {
			out.Bands[i] = clamp(cfg.Bands[i], -12, 12)
		}
	}
	if out.Preset == "" {
		out.Preset = "flat"
	}
	if out.Mode != "peq" {
		out.Mode = "graphic"
	}
	for _, b := range cfg.PEQ {
		if len(out.PEQ) >= MaxPEQBands {
			break
		}
		t := b.Type
		if t != "lowshelf" && t != "highshelf" {
			t = "peaking"
		}
		out.PEQ = append(out.PEQ, PEQBand{
			Type: t,
			Freq: clamp(b.Freq, 20, 20000),
			Gain: clamp(b.Gain, -24, 24),
			Q:    clamp(b.Q, 0.1, 10),
		})
	}
	return out
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func dbToLin(db float64) float64 { return math.Pow(10, db/20) }

type biquad struct{ b0, b1, b2, a1, a2 float64 }

type bstate struct{ x1, x2, y1, y2 float64 }

func unity() biquad { return biquad{b0: 1, b1: 0, b2: 0, a1: 0, a2: 0} }

func peaking(db, f0, rate, q float64) biquad {
	if db == 0 || f0 >= rate/2 {
		return unity()
	}
	A := math.Pow(10, db/40)
	w0 := 2 * math.Pi * f0 / rate
	alpha := math.Sin(w0) / (2 * q)
	cw := math.Cos(w0)
	a0 := 1 + alpha/A
	return biquad{
		b0: (1 + alpha*A) / a0,
		b1: (-2 * cw) / a0,
		b2: (1 - alpha*A) / a0,
		a1: (-2 * cw) / a0,
		a2: (1 - alpha/A) / a0,
	}
}

func shelf(db, f0, rate, q float64, high bool) biquad {
	if db == 0 || f0 >= rate/2 {
		return unity()
	}
	A := math.Pow(10, db/40)
	w0 := 2 * math.Pi * f0 / rate
	cw := math.Cos(w0)
	sq := 2 * math.Sqrt(A)
	alpha := math.Sin(w0) / 2 * math.Sqrt((A+1/A)*(1/q-1)+2)
	var b0, b1, b2, a0, a1, a2 float64
	if high {
		b0 = A * ((A + 1) + (A-1)*cw + sq*alpha)
		b1 = -2 * A * ((A - 1) + (A+1)*cw)
		b2 = A * ((A + 1) + (A-1)*cw - sq*alpha)
		a0 = (A + 1) - (A-1)*cw + sq*alpha
		a1 = 2 * ((A - 1) - (A+1)*cw)
		a2 = (A + 1) - (A-1)*cw - sq*alpha
	} else {
		b0 = A * ((A + 1) - (A-1)*cw + sq*alpha)
		b1 = 2 * A * ((A - 1) - (A+1)*cw)
		b2 = A * ((A + 1) - (A-1)*cw - sq*alpha)
		a0 = (A + 1) + (A-1)*cw + sq*alpha
		a1 = -2 * ((A - 1) + (A+1)*cw)
		a2 = (A + 1) + (A-1)*cw - sq*alpha
	}
	return biquad{b0: b0 / a0, b1: b1 / a0, b2: b2 / a0, a1: a1 / a0, a2: a2 / a0}
}

// design builds the filter cascade for a config at a sample rate.
func design(cfg Config, rate int) []biquad {
	if cfg.Mode == "peq" {
		out := make([]biquad, 0, len(cfg.PEQ))
		for _, b := range cfg.PEQ {
			switch b.Type {
			case "lowshelf":
				out = append(out, shelf(b.Gain, b.Freq, float64(rate), b.Q, false))
			case "highshelf":
				out = append(out, shelf(b.Gain, b.Freq, float64(rate), b.Q, true))
			default:
				out = append(out, peaking(b.Gain, b.Freq, float64(rate), b.Q))
			}
		}
		return out
	}
	out := make([]biquad, BandCount)
	for i, f0 := range BandCenters {
		out[i] = peaking(cfg.Bands[i], f0, float64(rate), bandQ)
	}
	return out
}

func lerp(a, b biquad, t float64) biquad {
	return biquad{
		b0: a.b0 + (b.b0-a.b0)*t,
		b1: a.b1 + (b.b1-a.b1)*t,
		b2: a.b2 + (b.b2-a.b2)*t,
		a1: a.a1 + (b.a1-a.a1)*t,
		a2: a.a2 + (b.a2-a.a2)*t,
	}
}

// softLimit keeps boosted samples below full scale with a tanh knee above -1 dBFS.
func softLimit(x float64) float64 {
	const t = 0.8912509381337456
	ax := math.Abs(x)
	if ax <= t {
		return x
	}
	over := (ax - t) / (1 - t)
	y := t + (1-t)*math.Tanh(over)
	if x < 0 {
		return -y
	}
	return y
}

// Engine is a mutex-protected EQ processor for one playback stream.
type Engine struct {
	mu       sync.Mutex
	enabled  bool
	rate     int
	channels int
	format   zat.Format
	preamp   float64
	target   []biquad
	cur      []biquad
	old      []biquad
	rampLeft int
	rampTot  int
	st       [][]bstate
}

// NewEngine builds an engine for a stream. bypass forces the EQ off (bit-perfect).
func NewEngine(cfg Config, rate, channels int, format zat.Format, bypass bool) *Engine {
	cfg = Normalize(cfg)
	e := &Engine{rate: rate, channels: channels, format: format}
	e.target = design(cfg, rate)
	e.cur = e.target
	e.old = e.target
	e.allocState()
	e.preamp = dbToLin(cfg.Preamp)
	e.enabled = cfg.Enabled && !bypass
	return e
}

func (e *Engine) allocState() {
	e.st = make([][]bstate, len(e.target))
	for b := range e.st {
		e.st[b] = make([]bstate, e.channels)
	}
}

// Enabled reports whether the EQ is currently applied.
func (e *Engine) Enabled() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.enabled
}

// Reset clears filter memory (seek / track change) without changing coefficients.
func (e *Engine) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for b := range e.st {
		for c := range e.st[b] {
			e.st[b][c] = bstate{}
		}
	}
	e.rampLeft = 0
	e.cur = e.target
}

// Reconfigure hot-swaps the curve with a short coefficient ramp to avoid clicks.
func (e *Engine) Reconfigure(cfg Config, rate int, bypass bool) {
	cfg = Normalize(cfg)
	e.mu.Lock()
	defer e.mu.Unlock()
	next := design(cfg, rate)
	if rate != e.rate || len(next) != len(e.target) {
		e.rate = rate
		e.target = next
		e.cur = next
		e.old = next
		e.rampLeft = 0
		e.allocState()
	} else {
		e.old = e.cur
		e.target = next
		e.rampTot = rate / 20
		if e.rampTot < 1 {
			e.rampTot = 1
		}
		e.rampLeft = e.rampTot
	}
	e.preamp = dbToLin(cfg.Preamp)
	e.enabled = cfg.Enabled && !bypass
}

// Process applies the EQ to interleaved little-endian PCM in place.
func (e *Engine) Process(pcm []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.enabled || len(pcm) == 0 || len(e.cur) == 0 {
		return
	}
	bps := e.format.BytesPerSample()
	if bps == 0 || e.channels <= 0 {
		return
	}
	frameBytes := bps * e.channels
	n := len(pcm) / frameBytes
	nb := len(e.cur)
	for i := 0; i < n; i++ {
		var coef []biquad
		if e.rampLeft > 0 {
			t := 1 - float64(e.rampLeft)/float64(e.rampTot)
			coef = make([]biquad, nb)
			for b := 0; b < nb; b++ {
				coef[b] = lerp(e.old[b], e.target[b], t)
			}
			e.rampLeft--
			if e.rampLeft == 0 {
				e.cur = e.target
			}
		} else {
			coef = e.cur
		}
		for ch := 0; ch < e.channels; ch++ {
			off := i*frameBytes + ch*bps
			x := decodeSample(pcm[off:off+bps], e.format) * e.preamp
			for b := 0; b < nb; b++ {
				s := &e.st[b][ch]
				c := coef[b]
				y := c.b0*x + c.b1*s.x1 + c.b2*s.x2 - c.a1*s.y1 - c.a2*s.y2
				s.x2, s.x1 = s.x1, x
				s.y2, s.y1 = s.y1, y
				x = y
			}
			encodeSample(pcm[off:off+bps], e.format, softLimit(x))
		}
	}
}

// ResponseAt returns the cascade magnitude in dB at freq (for verification).
func (e *Engine) ResponseAt(freq float64) float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	w := 2 * math.Pi * freq / float64(e.rate)
	cr, ci := math.Cos(w), -math.Sin(w)
	c2r, c2i := math.Cos(2*w), -math.Sin(2*w)
	mag := e.preamp
	for _, c := range e.target {
		nr := c.b0 + c.b1*cr + c.b2*c2r
		ni := c.b1*ci + c.b2*c2i
		dr := 1 + c.a1*cr + c.a2*c2r
		di := c.a1*ci + c.a2*c2i
		nm := math.Hypot(nr, ni)
		dm := math.Hypot(dr, di)
		if dm > 0 {
			mag *= nm / dm
		}
	}
	if mag <= 0 {
		return -120
	}
	return 20 * math.Log10(mag)
}

func decodeSample(b []byte, f zat.Format) float64 {
	switch f {
	case zat.S16LE:
		return float64(int16(uint16(b[0])|uint16(b[1])<<8)) / 32768
	case zat.S24LE:
		v := int32(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16)
		if v&0x800000 != 0 {
			v |= -0x1000000
		}
		return float64(v) / 8388608
	case zat.S32LE:
		return float64(int32(uint32(b[0])|uint32(b[1])<<8|uint32(b[2])<<16|uint32(b[3])<<24)) / 2147483648
	}
	return 0
}

func encodeSample(b []byte, f zat.Format, x float64) {
	switch f {
	case zat.S16LE:
		v := int32(math.Round(x * 32768))
		v = int32(clamp(float64(v), -32768, 32767))
		b[0] = byte(v)
		b[1] = byte(v >> 8)
	case zat.S24LE:
		v := int32(math.Round(x * 8388608))
		v = int32(clamp(float64(v), -8388608, 8388607))
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
	case zat.S32LE:
		v := int64(math.Round(x * 2147483648))
		if v > 2147483647 {
			v = 2147483647
		}
		if v < -2147483648 {
			v = -2147483648
		}
		u := uint32(int32(v))
		b[0] = byte(u)
		b[1] = byte(u >> 8)
		b[2] = byte(u >> 16)
		b[3] = byte(u >> 24)
	}
}