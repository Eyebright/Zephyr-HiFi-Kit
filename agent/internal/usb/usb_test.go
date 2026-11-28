package usb

import (
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
)

// builder assembles a configuration descriptor blob the way a firmware would:
// every entry is a length byte, a type byte and then the payload.
type builder struct{ b []byte }

func (t *builder) add(typ byte, payload ...byte) {
	t.b = append(t.b, byte(2+len(payload)), typ)
	t.b = append(t.b, payload...)
}

// standardInterface writes a bLength 9 interface descriptor.
func (t *builder) standardInterface(number, alt, numEP byte, class, sub, proto byte) {
	t.b = append(t.b, 9, dtInterface, number, alt, numEP, class, sub, proto, 0)
}

// standardEndpoint writes a bLength 7 endpoint descriptor.
func (t *builder) standardEndpoint(addr, attr byte, words uint16, interval byte) {
	t.b = append(t.b, 7, dtEndpoint, addr, byte(attr), byte(words), byte(words>>8), interval)
}

// highSpeedEndpoint writes the 9-byte audio endpoint descriptor high-speed uses,
// which adds bRefresh and the feedback endpoint address.
func (t *builder) highSpeedEndpoint(addr, attr byte, words uint16, interval, refresh, synch byte) {
	t.b = append(t.b, 9, dtEndpoint, addr, byte(attr), byte(words), byte(words>>8), interval, refresh, synch)
}

// patchLength stores the blob size in the configuration descriptor header.
func (t *builder) patchLength() []byte {
	b := append([]byte(nil), t.b...)
	b[2] = byte(len(b))
	b[3] = byte(len(b) >> 8)
	return b
}

func deviceDescriptor(bcdUSB uint16, ep0 byte) []byte {
	return []byte{18, dtDevice,
		byte(bcdUSB), byte(bcdUSB >> 8),
		0x00, 0x00, 0x00, ep0,
		0x6d, 0x2a, 0x00, 0x08, 0x00, 0x01,
		1, 2, 3, 1}
}

// uac2DAC builds a two-channel S32/S16 asynchronous high-speed output, the
// layout of an XMOS-based desktop DAC: one programmable clock source, a feature
// unit with writable master volume, and an AudioStreaming interface whose three
// alternate settings carry no rates at all.
func uac2DAC() []byte {
	t := &builder{}
	t.b = append(t.b, 9, dtConfiguration, 0, 0, 2, 1, 0, 0xc0, 250)

	// AudioControl interface 0.
	t.standardInterface(0, 0, 0, classAudio, subclassControl, 0x20)
	t.add(dtCSInterface, subHeader, 0x00, 0x02, 0x01, 0x3b, 0x00, 0x00) // bcdADC 2.0
	t.add(dtCSInterface, subClockSource, 0x28, 0x07, 0x03, 0x00, 0x00)  // internal programmable, synced to SOF, freq read+write
	t.add(dtCSInterface, subInputTerm, 0x01, 0x01, 0x01, 0x00, 0x28, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00)
	t.add(dtCSInterface, subOutputTerm, 0x02, 0x03, 0x01, 0x00, 0x01, 0x28, 0x00)
	// Feature unit id 3 with a writable master volume and no master mute.
	t.add(dtCSInterface, subFeatureUnit, 0x03, 0x01, 0x0c, 0x00, 0x00, 0x00, 0x0c, 0x00, 0x00, 0x00, 0x0c, 0x00, 0x00, 0x00)

	// AudioStreaming interface 1, alts 0..2, each with the S32 or S16 format.
	for _, f := range []struct {
		alt     byte
		subslot byte
		bits    byte
	}{{0, 4, 32}, {1, 4, 32}, {2, 2, 16}} {
		t.standardInterface(1, f.alt, 1, classAudio, subclassStream, 0x20)
		t.add(dtCSInterface, subAsGeneral, 0x02, 0x00, 0x01, 0x07, 0x00, 0x00, 0x00, 0x02, 0x03, 0x00, 0x00, 0x00, 0x00)
		t.add(dtCSInterface, subFormatType, 0x01, f.subslot, f.bits)
		t.highSpeedEndpoint(0x01, 0x05, 0x0438, 1, 0x06, 0x82) // isoc, async, 1080 B, bRefresh 6, feedback ep 0x82
		t.add(dtCSEndpoint, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00)
	}
	return t.patchLength()
}

// uac1DAC builds a full-speed 16-bit stereo output that lists its rates inside
// the FORMAT_TYPE descriptor, the PCM2702-style layout.
func uac1DAC() []byte {
	t := &builder{}
	t.b = append(t.b, 9, dtConfiguration, 0, 0, 2, 1, 0, 0xc0, 50)

	t.standardInterface(0, 0, 0, classAudio, subclassControl, 0x00)
	t.add(dtCSInterface, subHeader, 0x00, 0x01, 0x09, 0x00, 0x01, 0x01) // bcdADC 1.0, one streaming interface
	t.add(dtCSInterface, subInputTerm, 0x01, 0x01, 0x01, 0x00, 0x02, 0x00, 0x00, 0x00)
	t.add(dtCSInterface, subOutputTerm, 0x02, 0x03, 0x01, 0x00, 0x01, 0x00)

	for _, alt := range []byte{0, 1} {
		t.standardInterface(1, alt, 1, classAudio, subclassStream, 0x00)
		t.add(dtCSInterface, subAsGeneral, 0x02, 0x01, 0x01, 0x00) // wFormatTag TYPE_I PCM
		f := []byte{subFormatType, 0x01, 0x02, 0x02, 0x10, 0x02}   // 2 ch, 2 B subframe, 16 bits, two rates
		for _, r := range []int{44100, 48000} {
			f = append(f, byte(r), byte(r>>8), byte(r>>16))
		}
		t.add(dtCSInterface, f...)
		t.standardEndpoint(0x01, 0x01, 0x0100, 1)         // isoc, no sync, 256 B, 1 ms
		t.add(dtCSEndpoint, 0x01, 0x01, 0x00, 0x00, 0x00) // bit 0: endpoint has a sampling-frequency control
	}
	return t.patchLength()
}

func TestParseUAC2(t *testing.T) {
	dev, err := Parse(deviceDescriptor(0x0200, 64), uac2DAC(), "Test DAC")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if dev.Spec != "uac2" || dev.Speed != "high" || !dev.Microframe {
		t.Fatalf("spec/speed = %q/%q/%v", dev.Spec, dev.Speed, dev.Microframe)
	}
	if len(dev.Streams) != 1 {
		t.Fatalf("streams = %d, want the one output function", len(dev.Streams))
	}
	s := dev.Streams[0]
	if s.Interface != 1 || s.AcInterface != 0 || s.Terminal != 0x02 || s.ClockID != 0x28 {
		t.Errorf("stream = %+v", s)
	}
	if s.Endpoint.Sync != "async" || s.Endpoint.FeedbackAddress != 0x82 {
		t.Errorf("endpoint = %+v, want async pacing on feedback 0x82", s.Endpoint)
	}
	if s.Endpoint.MaxPacket != 1080 || s.Endpoint.Mult != 1 {
		t.Errorf("endpoint sizing = %d/%d, want 1080/1", s.Endpoint.MaxPacket, s.Endpoint.Mult)
	}
	// Three alternate settings, each usable, with no rates before a probe.
	if len(s.Formats) != 3 {
		t.Fatalf("formats = %d: %+v", len(s.Formats), s.Formats)
	}
	for _, f := range s.Formats {
		if len(f.Rates) != 0 {
			t.Errorf("alt %d lists %v before a probe; UAC2 keeps rates in the clock", f.Alt, f.Rates)
		}
	}
	if got := s.Formats[1]; got.Alt != 1 || got.Bits != 32 || got.Subslot != 4 || got.Channels != 2 {
		t.Errorf("alt 1 = %+v, want S32_LE stereo in 4-byte subslots", got)
	}
	if got := s.Formats[2]; got.Alt != 2 || got.Bits != 16 || got.Subslot != 2 {
		t.Errorf("alt 2 = %+v, want S16_LE in 2-byte subslots", got)
	}
	// The clock was reached through the output terminal bTerminalLink names.
	if s.RateRequest == nil {
		t.Fatal("no rate request for a UAC2 stream")
	}
	r := *s.RateRequest
	if r.BMRequestType != 0xa1 || r.BRequest != 0x02 || r.WValue != 0x0100 || r.WIndex != 0x2800 {
		t.Errorf("rate request = %+v, want 0xa1/0x02/0x0100/0x2800", r)
	}
	if r.ExpectLen != rangeProbeLen {
		t.Errorf("rate request buffer = %d", r.ExpectLen)
	}
	// Clock and volume readouts for the self-test screen.
	if len(dev.Clocks) != 1 || dev.Clocks[0].Type != "internal-programmable" ||
		!dev.Clocks[0].SyncedToSof || !dev.Clocks[0].FreqControl {
		t.Errorf("clocks = %+v", dev.Clocks)
	}
	if s.Volume == nil || !s.Volume.MasterVol || s.Volume.MasterMute || s.Volume.UnitID != 3 {
		t.Errorf("volume = %+v, want writable master volume only", s.Volume)
	}
}

func TestNegotiateUAC2(t *testing.T) {
	dev, err := Parse(deviceDescriptor(0x0200, 64), uac2DAC(), "Test DAC")
	if err != nil {
		t.Fatal(err)
	}
	rangeHex := hex.EncodeToString(rateRangePayload(44100, 48000, 96000, 192000))
	p, err := Negotiate(dev, Observation{Interface: 1, Bits: 32, Rate: 192000, RangeHex: rangeHex})
	if err != nil {
		t.Fatalf("Negotiate: %v", err)
	}
	if p.Alt != 1 || p.Subslot != 4 || p.Channels != 2 || p.SampleBytes != 8 {
		t.Errorf("plan = alt %d, %d B subslot x %d ch, want alt 1 S32 stereo", p.Alt, p.Subslot, p.Channels)
	}
	// 192000 Hz x 8 B = 1536000 B/s over 8000 microframes: exact, 192 B each.
	if p.BytesPerSecond != 1536000 || p.FramesPerSecond != 8000 || p.NominalBytes != 192 || p.RemainderBytes != 0 {
		t.Errorf("sizing = %d B/s over %d frames, %d+%d", p.BytesPerSecond, p.FramesPerSecond, p.NominalBytes, p.RemainderBytes)
	}
	if p.MaxPacketBytes != 1080 {
		t.Errorf("max packet bytes = %d", p.MaxPacketBytes)
	}
	if len(p.Requests) != 1 {
		t.Fatalf("requests = %+v, want only the clock rate", p.Requests)
	}
	r := p.Requests[0]
	// SET_CUR SAMPLE_FREQ_CONTROL to the AC interface, clock 0x28 in the high byte.
	if r.BMRequestType != 0x21 || r.BRequest != 0x01 || r.WValue != 0x0100 || r.WIndex != 0x2800 {
		t.Errorf("set-rate = %+v, want 0x21/0x01/0x0100/0x2800", r)
	}
	if r.Data != "00ee0200" { // 192000 = 0x0002EE00 little-endian
		t.Errorf("set-rate payload = %q, want 00ee0200", r.Data)
	}
	for _, n := range p.Notes {
		if strings.Contains(n, "remainder") {
			t.Errorf("unexpected remainder note for an exact rate: %q", n)
		}
	}

	// 44.1 kHz does not divide into the 125 µs microframe, so the plan must say
	// the pump has to alternate packet sizes.
	p2, err := Negotiate(dev, Observation{Interface: 1, Bits: 32, Rate: 44100, RangeHex: rangeHex})
	if err != nil {
		t.Fatalf("Negotiate 44.1k: %v", err)
	}
	if p2.NominalBytes != 44 || p2.RemainderBytes != 800 {
		t.Errorf("44.1k sizing = %d+%d B per microframe, want 44+800", p2.NominalBytes, p2.RemainderBytes)
	}
	if len(p2.Notes) < 3 {
		t.Fatalf("44.1k notes = %q, want fractional, remainder and async pacing", p2.Notes)
	}
	joined := strings.Join(p2.Notes, "\n")
	for _, want := range []string{"whole", "accumulate", "feedback endpoint 0x82"} {
		if !strings.Contains(joined, want) {
			t.Errorf("notes missing %q:\n%s", want, joined)
		}
	}

	// 16-bit requests must land on the other alternate setting.
	p3, err := Negotiate(dev, Observation{Interface: 1, Bits: 16, Rate: 48000, RangeHex: rangeHex})
	if err != nil {
		t.Fatalf("Negotiate S16: %v", err)
	}
	if p3.Alt != 2 || p3.Subslot != 2 || p3.NominalBytes != 24 {
		t.Errorf("S16 plan = alt %d, %d B subslot, %d B per microframe", p3.Alt, p3.Subslot, p3.NominalBytes)
	}
	// Alt 0 is the zero-bandwidth setting and is never selected.
	if p3.Alt == 0 || p.Alt == 0 {
		t.Error("alternate 0 must not be planned")
	}
}

func TestNegotiateUAC2UnknownRate(t *testing.T) {
	dev, _ := Parse(deviceDescriptor(0x0200, 64), uac2DAC(), "Test DAC")
	if _, err := Negotiate(dev, Observation{Interface: 1, Rate: 44100}); err == nil {
		t.Fatal("Negotiate accepted a rate the descriptors cannot supply")
	} else if !strings.Contains(err.Error(), "no rates") {
		t.Errorf("error = %v, want the probe-first hint", err)
	}
	if _, err := Negotiate(dev, Observation{Interface: 1, Bits: 24, RangeHex: hex.EncodeToString(rateRangePayload(44100))}); err == nil {
		t.Fatal("Negotiate accepted 24-bit output the device does not offer")
	}
	if _, err := Negotiate(dev, Observation{Interface: 7, RangeHex: hex.EncodeToString(rateRangePayload(44100))}); err == nil {
		t.Error("Negotiate accepted an interface that is not a stream")
	}
}

func TestSetRatesAndBest(t *testing.T) {
	dev, err := Parse(deviceDescriptor(0x0200, 64), uac2DAC(), "Test DAC")
	if err != nil {
		t.Fatal(err)
	}
	if err := dev.SetRates(1, []int{48000, 44100, 192000, 44100}); err != nil {
		t.Fatalf("SetRates: %v", err)
	}
	if dev.Streams[0].RateRequest != nil {
		t.Error("RateRequest survived a completed probe")
	}
	iface, alt, rate, bits, ch := dev.Best()
	if iface != 1 || alt != 1 || rate != 192000 || bits != 32 || ch != 2 {
		t.Errorf("Best = iface %d alt %d %d Hz %d bit %d ch", iface, alt, rate, bits, ch)
	}
	// Caps feeds the ZAT HELLO message and must list the probed table once.
	caps := dev.Caps()
	if caps["spec"] != "uac2" || caps["channels"] != 2 {
		t.Errorf("caps = %v", caps)
	}
	list, _ := caps["rates"].([]int)
	if len(list) != 3 {
		t.Fatalf("caps rates = %v, want the three probed rates once each", list)
	}
	p, err := Negotiate(dev, Observation{Interface: 1, Rate: 44100})
	if err != nil {
		t.Fatalf("Negotiate after SetRates: %v", err)
	}
	if p.Rate != 44100 {
		t.Errorf("plan rate = %d", p.Rate)
	}
	if err := dev.SetRates(9, []int{44100}); err == nil {
		t.Error("SetRates accepted an unknown interface")
	}
	if err := dev.SetRates(1, nil); err == nil {
		t.Error("SetRates accepted an empty table")
	}
}

func TestParseUAC1(t *testing.T) {
	dev, err := Parse(deviceDescriptor(0x0110, 8), uac1DAC(), "Old DAC")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if dev.Spec != "uac1" || dev.Speed != "full" || dev.Microframe {
		t.Fatalf("spec/speed = %q/%q/%v", dev.Spec, dev.Speed, dev.Microframe)
	}
	s := dev.Streams[0]
	if s.RateRequest != nil {
		t.Errorf("UAC1 stream carries a GET_RANGE request: %+v", s.RateRequest)
	}
	if len(s.Formats) != 2 {
		t.Fatalf("formats = %+v", s.Formats)
	}
	f := s.Formats[1]
	if f.Alt != 1 || f.Bits != 16 || f.Subslot != 2 || f.Channels != 2 {
		t.Errorf("alt 1 = %+v", f)
	}
	// The rates live in the FORMAT_TYPE descriptor for UAC1.
	if got := joinRates(f.Rates); got != "44100 48000" {
		t.Errorf("rates = %s, want the two discrete frequencies", got)
	}
	if s.Endpoint.Sync != "none" || s.Endpoint.MaxPacket != 256 {
		t.Errorf("endpoint = %+v", s.Endpoint)
	}
	if len(dev.Clocks) != 0 {
		t.Errorf("clocks = %+v, UAC1 has none", dev.Clocks)
	}

	p, err := Negotiate(dev, Observation{Interface: 1, Rate: 44100})
	if err != nil {
		t.Fatalf("Negotiate: %v", err)
	}
	// 44100 Hz x 4 B = 176400 B/s over 1000 full-speed frames: 176 B plus 400 B
	// of remainder, i.e. 100 extra samples a second.
	if p.FramesPerSecond != 1000 || p.NominalBytes != 176 || p.RemainderBytes != 400 {
		t.Errorf("sizing = %d frames, %d+%d B", p.FramesPerSecond, p.NominalBytes, p.RemainderBytes)
	}
	r := p.Requests[0]
	// Class, endpoint recipient, wValue = selector 1, wIndex = (ep << 8) | iface.
	if r.BMRequestType != 0x22 || r.BRequest != 0x01 || r.WValue != 0x0100 || r.WIndex != 0x0101 {
		t.Errorf("set-rate = %+v, want 0x22/0x01/0x0100/0x0101", r)
	}
	if r.Data != "44ac00" { // 44100 = 0x00AC44 little-endian, 3 bytes
		t.Errorf("set-rate payload = %q, want 44ac00", r.Data)
	}
	// 192 kHz is outside the discrete list.
	if _, err := Negotiate(dev, Observation{Interface: 1, Rate: 192000}); err == nil {
		t.Error("Negotiate planned a rate the UAC1 list does not contain")
	}
}

func TestDecodeRates(t *testing.T) {
	rs, err := DecodeRates(rateRangePayload(44100, 48000, 192000))
	if err != nil {
		t.Fatal(err)
	}
	if got := joinRates(rs); got != "44100 48000 192000" {
		t.Errorf("discrete triplets = %s", got)
	}

	// A triplet with min == max is one rate; a resolution of 0 is likewise one
	// rate even when the upper bound differs.
	rs, _ = DecodeRates(rateRangeTriplet(88200, 88200, 0, 96000, 192000, 0))
	if got := joinRates(rs); got != "88200 96000" {
		t.Errorf("single-value triplets = %s, want 88200 96000", got)
	}

	// A resolution of 1 Hz declares a continuous range: only standard audio
	// rates inside it are useful.
	rs, _ = DecodeRates(rateRangeTriplet(8000, 192000, 1))
	if got := joinRates(rs); got != "44100 48000 88200 96000 176400 192000" {
		t.Errorf("continuous range = %s", got)
	}

	// Stepped ranges expand, and a count that overruns the payload is truncated
	// rather than read past the end.
	rs, _ = DecodeRates(rateRangeTriplet(44100, 44108, 4))
	if got := joinRates(rs); got != "44100 44104 44108" {
		t.Errorf("stepped range = %s", got)
	}
	if _, err := DecodeRates([]byte{0x01}); err == nil {
		t.Error("DecodeRates accepted a payload with no count")
	}
	short := rateRangePayload(44100)
	if _, err := DecodeRates(short[:4]); err != nil {
		t.Errorf("truncated payload: %v", err)
	}
	if n := int(le16(short, 0)); n != 1 {
		t.Errorf("count = %d", n)
	}
}

func TestAltSupported(t *testing.T) {
	// Byte 0 is the size of the bitmap that follows, so alt 3 lives in byte 1.
	if !AltSupported([]byte{0x01, 0x08}, 3) {
		t.Error("alt 3 not read from a one-byte bitmap")
	}
	if AltSupported([]byte{0x01, 0x08}, 4) {
		t.Error("alt 4 reported supported")
	}
	if !AltSupported([]byte{0x02, 0x00, 0x01}, 8) {
		t.Error("alt 8 not read from a two-byte bitmap")
	}
	if AltSupported(nil, 0) || AltSupported([]byte{0x01}, 1) {
		t.Error("short bitmap reported supported")
	}
	q := ValidateAltRequest(1, 2)
	if q.BMRequestType != 0xa1 || q.BRequest != 0x01 || q.WValue != 0x0200 || q.WIndex != 1 || q.ExpectLen != 8 {
		t.Errorf("validate request = %+v", q)
	}
}

func TestParseRejectsNonAudio(t *testing.T) {
	dev := deviceDescriptor(0x0200, 64)

	// A UAC3 device has a clock model this package does not plan for.
	t2 := &builder{}
	t2.b = append(t2.b, 9, dtConfiguration, 0, 0, 1, 1, 0, 0xc0, 50)
	t2.standardInterface(0, 0, 0, classAudio, subclassControl, 0x30)
	t2.add(dtCSInterface, subHeader, 0x00, 0x03, 0x09, 0x00, 0x00)
	if _, err := Parse(dev, t2.patchLength(), "uac3"); err == nil || !strings.Contains(err.Error(), "0x0300") {
		t.Errorf("UAC3 error = %v, want the bcdADC named", err)
	}

	// An interface with no output endpoint is not a playback function.
	t3 := &builder{}
	t3.b = append(t3.b, 9, dtConfiguration, 0, 0, 1, 1, 0, 0xc0, 50)
	t3.standardInterface(0, 0, 0, classAudio, subclassControl, 0x20)
	t3.add(dtCSInterface, subHeader, 0x00, 0x02, 0x01, 0x3b, 0x00, 0x00)
	if _, err := Parse(dev, t3.patchLength(), "no stream"); err == nil || !strings.Contains(err.Error(), "streaming") {
		t.Errorf("empty error = %v, want the missing stream named", err)
	}

	// Wrong blobs must be refused rather than misread.
	if _, err := Parse(dev[:8], uac2DAC(), "short"); err != ErrUnsupported {
		t.Errorf("short device blob: %v", err)
	}
	if _, err := Parse(dev, append([]byte{0x09, dtInterface}, uac2DAC()[2:]...), "wrong type"); err != ErrUnsupported {
		t.Errorf("non-config blob: %v", err)
	}
	trunc := append([]byte(nil), uac2DAC()...)
	trunc = trunc[:len(trunc)-6]
	if _, err := Parse(dev, trunc, "truncated"); err == nil || !strings.Contains(err.Error(), "header says") {
		t.Errorf("truncated blob error = %v, want the length mismatch", err)
	}
	if _, err := Parse(dev, uac1DAC()[:9], "no interfaces"); err == nil {
		t.Error("a bare configuration header parsed")
	}
}

func TestFromHex(t *testing.T) {
	raw := uac2DAC()
	spaced := strings.ToUpper(hex.EncodeToString(raw))
	spaced = spaced[:40] + " " + spaced[40:] + "\n"
	back, err := FromHex(spaced)
	if err != nil {
		t.Fatalf("FromHex: %v", err)
	}
	if len(back) != len(raw) {
		t.Fatalf("FromHex returned %d bytes, want %d", len(back), len(raw))
	}
	dev, err := Parse(deviceDescriptor(0x0200, 64), back, "round-tripped")
	if err != nil {
		t.Fatalf("Parse after FromHex: %v", err)
	}
	if dev.Streams[0].ClockID != 0x28 {
		t.Errorf("clock = 0x%02x", dev.Streams[0].ClockID)
	}
	if _, err := FromHex("zz"); err == nil {
		t.Error("FromHex accepted non-hex")
	}
}

// rateRangePayload encodes a GET_RANGE reply of single-value triplets.
func rateRangePayload(rates ...int) []byte {
	out := []byte{byte(len(rates)), byte(len(rates) >> 8)}
	for _, r := range rates {
		out = append(out, byte(r), byte(r>>8), byte(r>>16), byte(r>>24))
		out = append(out, byte(r), byte(r>>8), byte(r>>16), byte(r>>24))
		out = append(out, 0, 0, 0, 0)
	}
	return out
}

// rateRangeTriplet encodes one arbitrary (min, max, resolution) subrange.
func rateRangeTriplet(vals ...int) []byte {
	out := []byte{byte(len(vals) / 3), 0}
	for i := 0; i+2 < len(vals); i += 3 {
		for _, v := range vals[i : i+3] {
			out = append(out, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
		}
	}
	return out
}

// joinRates renders a rate list for the comparisons above.
func joinRates(xs []int) string {
	parts := make([]string, len(xs))
	for i, r := range xs {
		parts[i] = strconv.Itoa(r)
	}
	return strings.Join(parts, " ")
}
