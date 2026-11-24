package dsp

import (
	"encoding/binary"
	"math"
	"testing"

	"hifihub/zat"
)

func sineS32(freq, rate, amp float64, n int) []byte {
	pcm := make([]byte, n*4)
	for i := 0; i < n; i++ {
		v := amp * math.Sin(2*math.Pi*freq*float64(i)/rate)
		u := uint32(int32(math.Round(v * 2147483648)))
		binary.LittleEndian.PutUint32(pcm[i*4:], u)
	}
	return pcm
}

func rmsS32(pcm []byte, from int) float64 {
	var sum float64
	var count int
	for i := from; i+4 <= len(pcm); i += 4 {
		v := float64(int32(binary.LittleEndian.Uint32(pcm[i:]))) / 2147483648
		sum += v * v
		count++
	}
	if count == 0 {
		return 0
	}
	return math.Sqrt(sum / float64(count))
}

func maxAbsS32(pcm []byte) float64 {
	var m float64
	for i := 0; i+4 <= len(pcm); i += 4 {
		v := math.Abs(float64(int32(binary.LittleEndian.Uint32(pcm[i:]))) / 2147483648)
		if v > m {
			m = v
		}
	}
	return m
}

func singleBandCfg(band int, db float64) Config {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Preset = "custom"
	cfg.Bands[band] = db
	return cfg
}

func TestFlatResponseIsUnity(t *testing.T) {
	e := NewEngine(DefaultConfig(), 48000, 2, zat.S32LE, false)
	e.mu.Lock()
	e.enabled = true
	e.mu.Unlock()
	for _, f := range []float64{31, 100, 1000, 8000, 16000} {
		if got := e.ResponseAt(f); math.Abs(got) > 0.01 {
			t.Fatalf("flat response at %v = %v dB, want ~0", f, got)
		}
	}
}

func TestSingleBandCenterGain(t *testing.T) {
	for band, f0 := range BandCenters {
		e := NewEngine(singleBandCfg(band, 6), 48000, 2, zat.S32LE, false)
		got := e.ResponseAt(f0)
		if math.Abs(got-6) > 0.1 {
			t.Fatalf("band %v center gain = %v dB, want ~6", band, got)
		}
	}
}

func TestProcessAppliesBandGain(t *testing.T) {
	const rate = 48000
	in := sineS32(1000, rate, 0.2, rate)
	want := rmsS32(in, rate/2)
	band := 0
	for i, center := range BandCenters {
		if center == 1000 {
			band = i
			break
		}
	}
	e := NewEngine(singleBandCfg(band, 6), rate, 1, zat.S32LE, false)
	out := append([]byte(nil), in...)
	e.Process(out)
	got := rmsS32(out, rate/2)
	ratio := got / want
	target := math.Pow(10, 6.0/20)
	if math.Abs(ratio-target)/target > 0.05 {
		t.Fatalf("1k band +6dB amplitude ratio = %v, want ~%v", ratio, target)
	}
}

func TestBypassAndDisabledLeavePCMUntouched(t *testing.T) {
	in := sineS32(1000, 48000, 0.5, 4096)
	for _, bypass := range []bool{true, false} {
		cfg := singleBandCfg(5, 12)
		if !bypass {
			cfg.Enabled = false
		}
		e := NewEngine(cfg, 48000, 1, zat.S32LE, bypass)
		out := append([]byte(nil), in...)
		e.Process(out)
		if string(out) != string(in) {
			t.Fatalf("bypass=%v disabled=%v modified PCM", bypass, !cfg.Enabled)
		}
	}
}

func TestSoftLimitPreventsOverflow(t *testing.T) {
	const rate = 48000
	in := sineS32(1000, rate, 0.9, rate)
	e := NewEngine(singleBandCfg(5, 12), rate, 1, zat.S32LE, false)
	out := append([]byte(nil), in...)
	e.Process(out)
	if m := maxAbsS32(out); m > 1.0 {
		t.Fatalf("limited output exceeded full scale: %v", m)
	}
	if rmsS32(out, rate/2) <= rmsS32(in, rate/2) {
		t.Fatalf("boost did not increase level")
	}
}

func TestReconfigureReturnsToFlat(t *testing.T) {
	e := NewEngine(singleBandCfg(5, 9), 48000, 1, zat.S32LE, false)
	e.Reconfigure(DefaultConfig(), 48000, false)
	// drain the ramp
	e.Process(make([]byte, 48000*4))
	if got := e.ResponseAt(1000); math.Abs(got) > 0.01 {
		t.Fatalf("after reconfigure to flat, 1k response = %v dB", got)
	}
}

func TestNormalizeClamps(t *testing.T) {
	cfg := Normalize(Config{Enabled: true, Preamp: 99, Bands: []float64{99, -99, 3}})
	if cfg.Preamp != 6 {
		t.Fatalf("preamp not clamped: %v", cfg.Preamp)
	}
	if cfg.Bands[0] != 12 || cfg.Bands[1] != -12 || cfg.Bands[2] != 3 {
		t.Fatalf("bands not clamped: %v", cfg.Bands)
	}
	if len(cfg.Bands) != BandCount {
		t.Fatalf("band count = %v", len(cfg.Bands))
	}
}

func TestFlatAndDisabledAreBitExactS24(t *testing.T) {
	const rate = 96000
	in := sineS32(997, rate, 0.4, 4096)
	// convert S32 sine to packed S24
	p24 := make([]byte, 0, len(in)/4*3)
	for i := 0; i+4 <= len(in); i += 4 {
		v := int32(binary.LittleEndian.Uint32(in[i:])) >> 8
		u := uint32(v)
		p24 = append(p24, byte(u), byte(u>>8), byte(u>>16))
	}
	for _, enabled := range []bool{false, true} {
		cfg := DefaultConfig()
		cfg.Enabled = enabled
		e := NewEngine(cfg, rate, 1, zat.S24LE, false)
		out := append([]byte(nil), p24...)
		e.Process(out)
		if string(out) != string(p24) {
			t.Fatalf("enabled=%v flat EQ altered 24-bit PCM", enabled)
		}
	}
}

func TestPEQPeakingCenterGain(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Mode = "peq"
	cfg.PEQ = []PEQBand{{Type: "peaking", Freq: 1000, Gain: 6, Q: 1}}
	e := NewEngine(cfg, 48000, 1, zat.S32LE, false)
	if got := e.ResponseAt(1000); math.Abs(got-6) > 0.1 {
		t.Fatalf("PEQ peaking 1k gain = %v, want ~6", got)
	}
}

func TestPEQShelves(t *testing.T) {
	low := DefaultConfig()
	low.Enabled = true
	low.Mode = "peq"
	low.PEQ = []PEQBand{{Type: "lowshelf", Freq: 100, Gain: 6, Q: 0.7}}
	e := NewEngine(low, 48000, 1, zat.S32LE, false)
	if got := e.ResponseAt(20); math.Abs(got-6) > 1 {
		t.Fatalf("low shelf low-freq gain = %v, want ~6", got)
	}
	if got := e.ResponseAt(5000); math.Abs(got) > 0.5 {
		t.Fatalf("low shelf high-freq gain = %v, want ~0", got)
	}
	high := DefaultConfig()
	high.Enabled = true
	high.Mode = "peq"
	high.PEQ = []PEQBand{{Type: "highshelf", Freq: 8000, Gain: 6, Q: 0.7}}
	e2 := NewEngine(high, 48000, 1, zat.S32LE, false)
	if got := e2.ResponseAt(16000); math.Abs(got-6) > 1 {
		t.Fatalf("high shelf high-freq gain = %v, want ~6", got)
	}
	if got := e2.ResponseAt(100); math.Abs(got) > 0.5 {
		t.Fatalf("high shelf low-freq gain = %v, want ~0", got)
	}
}

func TestPresetsWellFormed(t *testing.T) {
	if len(Presets) < 5 {
		t.Fatalf("expected several reference presets, got %d", len(Presets))
	}
	for _, id := range []string{"pop", "jazz", "country", "classical"} {
		if _, ok := PresetByID(id); !ok {
			t.Fatalf("missing requested preset %s", id)
		}
	}
	for _, p := range Presets {
		if len(p.Bands) != BandCount {
			t.Fatalf("preset %s band count = %d", p.ID, len(p.Bands))
		}
		if _, ok := PresetByID(p.ID); !ok {
			t.Fatalf("preset %s not lookupable", p.ID)
		}
	}
}