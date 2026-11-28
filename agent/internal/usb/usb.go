// Package usb turns a raw USB descriptor blob into the control transfers needed
// to drive an audio DAC directly, bypassing the host audio stack.
//
// It exists because the Android terminal can only reach a USB device through the
// Java USB host API: Kotlin executes the requests returned here and owns the
// isochronous pump, while every class-specific offset, selector and framing
// number stays in Go where it can be tested on a host. Descriptor layouts follow
// the USB Audio Class 1.0 and 2.0 definitions as reflected by the Linux kernel
// headers <linux/usb/audio.h> and <linux/usb/audio-v2.h>.
package usb

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Standard descriptor types inside a configuration blob.
const (
	dtDevice        = 0x01
	dtConfiguration = 0x02
	dtInterface     = 0x04
	dtEndpoint      = 0x05
	dtCSInterface   = 0x24
	dtCSEndpoint    = 0x25
)

// Audio class codes and class-specific control request encoding.
const (
	classAudio      = 0x01
	subclassControl = 0x01
	subclassStream  = 0x02

	dirIn         = byte(0x80)
	typeClass     = byte(0x01 << 5)
	rcptInterface = byte(0x01)
	rcptEndpoint  = byte(0x02)

	reqSetCur   = byte(0x01)
	reqGetCur   = byte(0x01)
	reqGetRange = byte(0x02)

	uac1FreqSel     = 0x01 // UAC1 SAMPLE_FREQ_CONTROL on the endpoint
	uac2FreqSel     = 0x01 // UAC2_CS_CONTROL_SAM_FREQ on the clock source
	uac2ValidateAlt = 0x02 // UAC2_AS_CONTROL_VALID_ALTERNATE_SETTINGS
	rangeProbeLen   = 512  // GET_RANGE buffer; enough for 42 subranges
)

// Class-specific interface descriptor subtypes. AudioControl and AudioStreaming
// share the numbering, so the subclass of the interface being walked decides the
// meaning.
const (
	subHeader      = 0x01
	subAsGeneral   = 0x01
	subInputTerm   = 0x02
	subOutputTerm  = 0x03
	subFeatureUnit = 0x06
	subFormatType  = 0x02
	subClockSource = 0x0a
)

// standardRates are the audio rates the terminal may ask for. A device that
// declares a continuous range is matched against these instead of being stepped
// through every hertz of it.
var standardRates = []int{44100, 48000, 88200, 96000, 176400, 192000, 352800, 384000}

// ErrUnsupported reports a blob that is not a USB audio configuration descriptor,
// including UAC3 devices whose clock model this package does not plan for.
var ErrUnsupported = errors.New("usb: unsupported audio class descriptor")

// Request is one control transfer, replayed verbatim by Kotlin through
// UsbDeviceConnection.controlTransfer. Data is the hex payload of an OUT request;
// ExpectLen is the buffer size of an IN request.
type Request struct {
	Label         string `json:"label"`
	BMRequestType byte   `json:"bmRequestType"`
	BRequest      byte   `json:"bRequest"`
	WValue        int    `json:"wValue"`
	WIndex        int    `json:"wIndex"`
	Data          string `json:"data,omitempty"`
	ExpectLen     int    `json:"expectLen,omitempty"`
}

// Endpoint is an isochronous audio data endpoint. Sync names the pacing model:
// an async endpoint must be clocked from its feedback endpoint, while none and
// adaptive can be driven from the nominal byte count alone.
type Endpoint struct {
	Address         int    `json:"address"`
	Attributes      int    `json:"attributes"`
	MaxPacket       int    `json:"maxPacket"`
	Mult            int    `json:"mult"`
	Interval        int    `json:"interval"`
	Sync            string `json:"sync"`
	FeedbackAddress int    `json:"feedbackAddress,omitempty"`
}

// Format is one alternate setting of an AudioStreaming interface. Rates stays
// empty for UAC2 until a GET_RANGE probe or SetRates fills it in.
type Format struct {
	Alt      int   `json:"alt"`
	Bits     int   `json:"bits"`
	Subslot  int   `json:"subslot"`
	Channels int   `json:"channels"`
	Rates    []int `json:"rates,omitempty"`
}

// Clock is a UAC2 clock source unit. FreqControl marks the clocks whose
// bmControls makes the sample rate writable, which is what allows a rate change
// without a reset.
type Clock struct {
	ID          int    `json:"id"`
	Type        string `json:"type"`
	SyncedToSof bool   `json:"syncedToSof"`
	FreqControl bool   `json:"freqControl"`
}

// Volume reports a UAC2 feature unit with writable master controls, i.e. the
// hardware volume the terminal could use instead of soft volume.
type Volume struct {
	UnitID     int  `json:"unitId"`
	Interface  int  `json:"interface"`
	MasterVol  bool `json:"masterVolume"`
	MasterMute bool `json:"masterMute"`
}

// Stream is one AudioStreaming interface with all of its alternate settings and
// the data endpoint they share.
type Stream struct {
	Interface   int      `json:"interface"`
	AcInterface int      `json:"acInterface"`
	Terminal    int      `json:"terminalLink"`
	ClockID     int      `json:"clockId,omitempty"`
	Endpoint    Endpoint `json:"endpoint"`
	Formats     []Format `json:"formats"`
	// RateRequest reads the rate table of a UAC2 stream. It is nil for UAC1,
	// which lists its rates inside the FORMAT_TYPE descriptor.
	RateRequest *Request `json:"rateRequest,omitempty"`
	Volume      *Volume  `json:"volume,omitempty"`
}

// Device is the audio function of one DAC as described by its descriptors.
type Device struct {
	Spec       string   `json:"spec"`
	Speed      string   `json:"speed"`
	Microframe bool     `json:"microframe"`
	Product    string   `json:"product,omitempty"`
	Streams    []Stream `json:"streams"`
	Clocks     []Clock  `json:"clocks,omitempty"`
}

// terminalByID maps an AudioControl terminal id to the clock source that
// governs its sampling rate, as read from a UAC2 terminal descriptor.
type terminalByID map[int]int

// alt is the mutable record of one interface alternate setting while walking.
type alt struct {
	iface     int
	acIface   int
	number    int
	link      int
	bits      int
	subslot   int
	ch        int
	rates     []int
	needsRate bool
	ep        *Endpoint
}

// walker holds the state a single descriptor pass accumulates.
type walker struct {
	dev      *Device
	spec     string
	bcdADC   int
	acIface  int
	clocks   map[int]Clock
	terms    terminalByID
	volume   *Volume
	alts     []*alt
	cur      *alt // streaming alternate being decoded; nil on an AC interface
	inStream bool
}

// Parse reads a device descriptor (at least its first 18 bytes) and a full
// configuration descriptor blob. product is only echoed back for the UI; Android
// already knows the human-readable names.
func Parse(devBlob, cfgBlob []byte, product string) (*Device, error) {
	if len(devBlob) < 18 || devBlob[1] != dtDevice {
		return nil, ErrUnsupported
	}
	if len(cfgBlob) < 9 || cfgBlob[1] != dtConfiguration {
		return nil, ErrUnsupported
	}
	if int(le16(cfgBlob, 2)) != len(cfgBlob) {
		return nil, fmt.Errorf("usb: config blob is %d bytes, its header says %d", len(cfgBlob), le16(cfgBlob, 2))
	}
	w := &walker{
		dev:    &Device{Product: product, Speed: speedName(devBlob)},
		clocks: map[int]Clock{},
		terms:  terminalByID{},
	}
	w.dev.Microframe = w.dev.Speed != "full"

	for i := 0; i+1 < len(cfgBlob); {
		n := int(cfgBlob[i])
		if n < 2 || i+n > len(cfgBlob) {
			return nil, fmt.Errorf("usb: truncated descriptor at offset %d", i)
		}
		d := cfgBlob[i : i+n]
		switch d[1] {
		case dtInterface:
			if n >= 9 {
				w.enterInterface(d)
			}
		case dtEndpoint:
			if n >= 7 {
				w.addEndpoint(d)
			}
		case dtCSInterface:
			w.classInterface(d)
		case dtCSEndpoint:
			// bmAttributes here refines lock and pitch behaviour that the
			// prototype does not act on.
		}
		i += n
	}

	switch w.spec {
	case "":
		return nil, ErrUnsupported
	case "uac3":
		return nil, fmt.Errorf("%w: bcdADC 0x%04x", ErrUnsupported, w.bcdADC)
	}
	w.dev.Spec = w.spec
	for _, iface := range w.streamOrder() {
		s := w.stream(iface)
		if s == nil {
			continue
		}
		w.dev.Streams = append(w.dev.Streams, *s)
	}
	if len(w.dev.Streams) == 0 {
		return nil, errors.New("usb: no audio streaming interface with an output endpoint")
	}
	for i := range w.dev.Streams {
		w.resolveClock(&w.dev.Streams[i])
	}
	for _, c := range w.clocks {
		w.dev.Clocks = append(w.dev.Clocks, c)
	}
	sort.Slice(w.dev.Clocks, func(i, j int) bool { return w.dev.Clocks[i].ID < w.dev.Clocks[j].ID })
	return w.dev, nil
}

func (w *walker) enterInterface(d []byte) {
	iface, number := int(d[2]), int(d[3])
	class, sub, proto := int(d[5]), int(d[6]), int(d[7])
	w.cur, w.inStream = nil, false
	if class != classAudio {
		return
	}
	switch sub {
	case subclassControl:
		w.acIface = iface
	case subclassStream:
		a := &alt{iface: iface, acIface: w.acIface, number: number}
		// Alternate settings repeat the class descriptors, but a device that
		// only spells out its format in alt 0 is still usable, so carry the
		// known fields forward within this interface.
		for _, prev := range w.alts {
			if prev.iface == iface {
				a.bits, a.subslot, a.ch, a.rates = prev.bits, prev.subslot, prev.ch, prev.rates
			}
		}
		w.alts = append(w.alts, a)
		w.cur, w.inStream = a, true
		if w.spec == "" {
			w.spec = specFromProtocol(proto)
		}
	}
}

func (w *walker) addEndpoint(d []byte) {
	attr := int(d[3])
	words := int(le16(d, 4))
	ep := Endpoint{
		Address:    int(d[2]),
		Attributes: attr,
		Interval:   int(d[6]),
		Sync:       syncName((attr >> 2) & 0x03),
	}
	if attr&0x03 == 1 { // isochronous
		ep.MaxPacket = words & 0x07ff
		ep.Mult = (words >> 11) & 0x03
		if w.dev.Microframe && ep.Mult == 0 {
			ep.Mult = 1
		}
	}
	if len(d) >= 9 {
		// High-speed audio endpoints carry bRefresh at [7] and the full
		// address of their feedback endpoint at [8], direction bit included.
		if syn := int(d[8]); syn != 0 {
			ep.FeedbackAddress = syn
		}
	}
	if w.cur == nil {
		return
	}
	switch {
	case w.cur.ep == nil && ep.Address&0x80 == 0:
		w.cur.ep = &ep
	case w.cur.ep != nil && ep.Address&0x80 != 0 && w.cur.ep.FeedbackAddress == 0:
		// A separate feedback endpoint, named by address rather than by the
		// high-speed synch field.
		w.cur.ep.FeedbackAddress = ep.Address
	}
}

// classInterface decodes the AudioControl units on the control interface, and the
// AS_GENERAL plus FORMAT_TYPE descriptors of the streaming alternate in play.
func (w *walker) classInterface(d []byte) {
	n, sub := int(d[0]), int(d[2])
	if n < 3 {
		return
	}
	if w.inStream {
		w.streamClass(d, sub, n)
		return
	}
	switch sub {
	case subHeader:
		if n >= 5 {
			w.bcdADC = int(le16(d, 3))
			if s := specFromBCD(w.bcdADC); s != "" {
				w.spec = s
			}
		}
	case subInputTerm:
		if w.spec == "uac2" && n >= 8 {
			w.terms[int(d[3])] = int(d[7]) // bTerminalID ← bCSourceID
		}
	case subOutputTerm:
		if w.spec == "uac2" && n >= 9 {
			w.terms[int(d[3])] = int(d[8])
		}
	case subFeatureUnit:
		if w.spec != "uac2" || n < 9 {
			return
		}
		// bmaControls is a (channels+1)-element array of 4-byte bitmaps,
		// entry 0 being the master. Each control owns a 2-bit code: 01b
		// present and readable, 11b also host-programmable; the value is
		// mute at bits 0..1 and volume at bits 2..3.
		master := le32(d, 5)
		if w.volume == nil && master != 0 {
			w.volume = &Volume{
				UnitID:     int(d[3]),
				Interface:  w.acIface,
				MasterMute: master&0x02 != 0,
				MasterVol:  (master>>2)&0x02 != 0,
			}
		}
	case subClockSource:
		if w.spec != "uac2" || n < 8 {
			return
		}
		// Clock Source: bClockID, bmAttributes (bits 1..0 type, bit 2
		// synced-to-SOF) and bmControls (pair 0 is the sample-frequency
		// control, pair 1 the clock-valid control).
		w.clocks[int(d[3])] = Clock{
			ID:          int(d[3]),
			Type:        clockName(int(d[4]) & 0x03),
			SyncedToSof: int(d[4])&0x04 != 0,
			FreqControl: int(d[5])&0x02 != 0,
		}
	}
}

func (w *walker) streamClass(d []byte, sub, n int) {
	a := w.cur
	if a == nil {
		return
	}
	switch sub {
	case subAsGeneral:
		if n >= 4 {
			a.link = int(d[3])
		}
		if w.spec == "uac2" && n >= 11 {
			a.ch = int(d[10])
		}
	case subFormatType:
		switch w.spec {
		case "uac1":
			// bFormatType 0x01 is TYPE_I discrete PCM; TYPE_II (general) and
			// TYPE_III (extensible) are not plain PCM and stay unplanned.
			if n >= 8 && int(d[3]) == 1 {
				a.ch = int(d[4])
				a.subslot = int(d[5])
				a.bits = int(d[6])
				a.rates = uac1Rates(d[7:], n-7)
			}
		case "uac2":
			// FORMAT_TYPE_I here is 6 bytes and carries no rates at all: they
			// live in the clock source and must be read with GET_RANGE.
			if n >= 6 && int(d[3]) == 1 {
				a.subslot = int(d[4])
				a.bits = int(d[5])
				a.needsRate = true
			}
		}
	}
}

// uac1Rates decodes the tail of a UAC1 FORMAT_TYPE_I descriptor: a bSamFreqType
// count followed by that many 3-byte little-endian frequencies, or, when the
// count is 0xFF, one continuous range.
func uac1Rates(tail []byte, size int) []int {
	if size < 1 {
		return nil
	}
	count := int(tail[0])
	if count == 0xff {
		if size < 7 {
			return nil
		}
		lo, hi := int(uac24(tail, 1)), int(uac24(tail, 4))
		var out []int
		for _, r := range standardRates {
			if r >= lo && r <= hi {
				out = append(out, r)
			}
		}
		return out
	}
	var out []int
	for i := 0; i < count; i++ {
		off := 1 + 3*i
		if off+3 > size {
			break
		}
		out = append(out, int(uac24(tail, off)))
	}
	sort.Ints(out)
	return dedupe(out)
}

// streamOrder lists the streaming interface numbers in the order they appeared.
func (w *walker) streamOrder() []int {
	var order []int
	for _, a := range w.alts {
		seen := false
		for _, i := range order {
			if i == a.iface {
				seen = true
			}
		}
		if !seen {
			order = append(order, a.iface)
		}
	}
	return order
}

// stream folds the alternate settings of one interface into a Stream, or returns
// nil when none of them has an output data endpoint.
func (w *walker) stream(iface int) *Stream {
	var s *Stream
	for _, a := range w.alts {
		if a.iface != iface {
			continue
		}
		if s == nil {
			if a.ep == nil {
				return nil
			}
			s = &Stream{
				Interface:   a.iface,
				AcInterface: a.acIface,
				Terminal:    a.link,
				Endpoint:    *a.ep,
				Volume:      w.volume,
			}
			if w.spec == "uac2" && a.needsRate {
				s.RateRequest = &Request{
					Label:         "rate-range",
					BMRequestType: dirIn | typeClass | rcptInterface,
					BRequest:      reqGetRange,
					WValue:        uac2FreqSel << 8,
					WIndex:        a.acIface, // patched with the clock id once resolved
					ExpectLen:     rangeProbeLen,
				}
			}
		}
		if a.ch == 0 || a.subslot == 0 {
			continue // an alternate with no FORMAT_TYPE cannot be planned
		}
		s.Formats = append(s.Formats, Format{
			Alt:      a.number,
			Bits:     a.bits,
			Subslot:  a.subslot,
			Channels: a.ch,
			Rates:    a.rates,
		})
	}
	return s
}

// resolveClock ties a UAC2 stream to the clock source named by the terminal its
// AS_GENERAL descriptor links to, and fixes the GET_RANGE wIndex accordingly. A
// device with exactly one writable clock is matched to it directly, which covers
// the common single-frequency DAC.
func (w *walker) resolveClock(s *Stream) {
	if w.spec != "uac2" {
		return
	}
	id := w.terms[s.Terminal]
	if _, ok := w.clocks[id]; !ok {
		var only Clock
		n := 0
		for _, c := range w.clocks {
			if c.FreqControl {
				only, n = c, n+1
			}
		}
		if n != 1 {
			return
		}
		id = only.ID
	}
	c := w.clocks[id]
	s.ClockID = c.ID
	if s.RateRequest != nil {
		s.RateRequest.WIndex = c.ID<<8 | s.AcInterface
	}
}

// SetRates records the rates a UAC2 probe returned for one interface so later
// calls to Negotiate need no raw payload.
func (d *Device) SetRates(iface int, rates []int) error {
	for i := range d.Streams {
		if d.Streams[i].Interface != iface {
			continue
		}
		sort.Ints(rates)
		rates = dedupe(rates)
		for j := range d.Streams[i].Formats {
			d.Streams[i].Formats[j].Rates = rates
		}
		d.Streams[i].RateRequest = nil
		if len(rates) == 0 {
			return fmt.Errorf("usb: interface %d probed no rates", iface)
		}
		return nil
	}
	return fmt.Errorf("usb: no stream on interface %d", iface)
}

// Observation tells Negotiate which stream to configure, what the device answered to
// its rate probe, and the format the caller asked for. A zero rate, bits or
// channel count means "whatever the device prefers".
type Observation struct {
	Interface int    `json:"interface"`
	Alt       int    `json:"alt,omitempty"`
	Rate      int    `json:"rate,omitempty"`
	Bits      int    `json:"bits,omitempty"`
	Channels  int    `json:"channels,omitempty"`
	RangeHex  string `json:"rangeHex,omitempty"`
}

// Plan is one exclusive session: the alternate setting to select, the transfers
// that enter it and the isochronous byte budget per (micro)frame.
type Plan struct {
	Interface       int       `json:"interface"`
	AcInterface     int       `json:"acInterface"`
	Alt             int       `json:"alt"`
	Rate            int       `json:"rate"`
	Bits            int       `json:"bits"`
	Subslot         int       `json:"subslot"`
	Channels        int       `json:"channels"`
	SampleBytes     int       `json:"sampleBytes"`
	BytesPerSecond  int       `json:"bytesPerSecond"`
	Speed           string    `json:"speed"`
	FramesPerSecond int       `json:"framesPerSecond"`
	NominalBytes    int       `json:"nominalBytes"`
	RemainderBytes  int       `json:"remainderBytes"` // bytes per second the nominal size cannot carry
	MaxPacketBytes  int       `json:"maxPacketBytes"`
	Endpoint        Endpoint  `json:"endpoint"`
	ClockID         int       `json:"clockId,omitempty"`
	Volume          *Volume   `json:"volume,omitempty"`
	Requests        []Request `json:"requests"`
	Notes           []string  `json:"notes,omitempty"`
}

// Negotiate picks the alternate setting that serves the requested format and
// returns the control transfers that put the DAC into it. Kotlin still owns
// SET_INTERFACE and the isochronous pump.
func Negotiate(dev *Device, obs Observation) (*Plan, error) {
	if dev == nil {
		return nil, ErrUnsupported
	}
	var s *Stream
	for i := range dev.Streams {
		if dev.Streams[i].Interface == obs.Interface {
			s = &dev.Streams[i]
		}
	}
	if s == nil {
		return nil, fmt.Errorf("usb: no stream on interface %d", obs.Interface)
	}
	rates := obs.rates(s)
	if len(rates) == 0 {
		return nil, fmt.Errorf("usb: interface %d has no rates yet; run its rate request first", obs.Interface)
	}
	if obs.Rate == 0 {
		obs.Rate = rates[len(rates)-1]
	}
	var chosen *Format
	for i := range s.Formats {
		f := &s.Formats[i]
		if f.Alt == 0 {
			continue
		}
		ok := contains(rates, obs.Rate)
		if obs.Alt != 0 {
			ok = ok && f.Alt == obs.Alt
		}
		if obs.Bits != 0 {
			ok = ok && f.Bits == obs.Bits
		}
		if obs.Channels != 0 {
			ok = ok && f.Channels == obs.Channels
		}
		if ok {
			chosen = f
			break
		}
	}
	if chosen == nil {
		return nil, fmt.Errorf("usb: no alternate setting for rate=%d bits=%d channels=%d", obs.Rate, obs.Bits, obs.Channels)
	}
	if chosen.Subslot == 0 || chosen.Channels == 0 {
		return nil, fmt.Errorf("usb: incomplete FORMAT_TYPE on alternate %d", chosen.Alt)
	}
	if dev.Spec == "uac2" && s.ClockID == 0 {
		return nil, fmt.Errorf("usb: interface %d has no clock source; cannot set its rate", s.Interface)
	}

	fps := 1000
	if dev.Microframe {
		fps = 8000
	}
	sampleBytes := chosen.Subslot * chosen.Channels
	bps := obs.Rate * sampleBytes
	p := &Plan{
		Interface:       s.Interface,
		AcInterface:     s.AcInterface,
		Alt:             chosen.Alt,
		Rate:            obs.Rate,
		Bits:            chosen.Bits,
		Subslot:         chosen.Subslot,
		Channels:        chosen.Channels,
		SampleBytes:     sampleBytes,
		BytesPerSecond:  bps,
		Speed:           dev.Speed,
		FramesPerSecond: fps,
		NominalBytes:    bps / fps,
		RemainderBytes:  bps % fps,
		Endpoint:        s.Endpoint,
		ClockID:         s.ClockID,
		Volume:          s.Volume,
	}
	// A UAC2 clock with no writable frequency control is fixed: the alternate
	// setting alone picks the rate and a SET_CUR would be rejected.
	if dev.Spec == "uac1" || dev.writableClock(s.ClockID) {
		p.Requests = append(p.Requests, rateRequest(dev.Spec, s, obs.Rate))
	} else {
		p.Notes = append(p.Notes, fmt.Sprintf(
			"clock %d is not host-programmable; alternate %d fixes the rate", s.ClockID, chosen.Alt))
	}
	p.MaxPacketBytes = s.Endpoint.MaxPacket * p.transactions()
	if p.NominalBytes > 0 && p.NominalBytes%p.SampleBytes != 0 {
		p.Notes = append(p.Notes, fmt.Sprintf(
			"%d B per frame is not a whole %d B sample: size packets by byte count and carry the remainder",
			p.NominalBytes, p.SampleBytes))
	}
	if p.RemainderBytes != 0 {
		p.Notes = append(p.Notes, fmt.Sprintf(
			"%d B/s over %d frames/s leaves %d B per second: accumulate the remainder and release whole samples as they build up",
			bps, fps, p.RemainderBytes))
	}
	if p.MaxPacketBytes > 0 && p.NominalBytes > p.MaxPacketBytes {
		p.Notes = append(p.Notes, fmt.Sprintf(
			"nominal %d B exceeds the %d B endpoint budget; submit %d transactions per frame",
			p.NominalBytes, p.MaxPacketBytes, (p.NominalBytes+p.MaxPacketBytes-1)/p.MaxPacketBytes))
	}
	if s.Endpoint.Sync == "async" {
		p.Notes = append(p.Notes, fmt.Sprintf(
			"async endpoint: pace from feedback endpoint 0x%02x, nominal sizing alone will drift",
			s.Endpoint.FeedbackAddress))
	}
	return p, nil
}

// transactions is bMult: high-speed audio may need more than one transaction per
// microframe for a given rate, encoded 1..3 as 0..2 in the descriptor.
func (p *Plan) transactions() int {
	if p.Endpoint.Mult < 1 {
		return 1
	}
	return p.Endpoint.Mult
}

// writableClock reports whether a clock's sample frequency is host-programmable.
func (d *Device) writableClock(id int) bool {
	for _, c := range d.Clocks {
		if c.ID == id {
			return c.FreqControl
		}
	}
	return false
}

func rateRequest(spec string, s *Stream, rate int) Request {
	if spec == "uac2" {
		data := make([]byte, 4)
		binary.LittleEndian.PutUint32(data, uint32(rate))
		return Request{
			Label:         "set-rate",
			BMRequestType: typeClass | rcptInterface,
			BRequest:      reqSetCur,
			WValue:        uac2FreqSel << 8,
			WIndex:        s.ClockID<<8 | s.AcInterface,
			Data:          hex.EncodeToString(data),
		}
	}
	return Request{
		Label:         "set-rate",
		BMRequestType: typeClass | rcptEndpoint,
		BRequest:      reqSetCur,
		WValue:        uac1FreqSel << 8,
		WIndex:        s.Endpoint.Address<<8 | s.Interface,
		Data:          fmt.Sprintf("%02x%02x%02x", byte(rate), byte(rate>>8), byte(rate>>16)),
	}
}

// ValidateAltRequest asks a UAC2 device whether an alternate setting is usable
// before any bandwidth is committed to it.
func ValidateAltRequest(iface, alt int) Request {
	return Request{
		Label:         "validate-alt",
		BMRequestType: dirIn | typeClass | rcptInterface,
		BRequest:      reqGetCur,
		WValue:        uac2ValidateAlt << 8,
		WIndex:        iface,
		ExpectLen:     8,
	}
}

// AltSupported reads the bitmap ValidateAltRequest returns: the first byte is
// the size of the bitmap that follows it, so alternate N lives in byte 1+N/8.
func AltSupported(payload []byte, alt int) bool {
	if alt < 0 || len(payload) < 1 {
		return false
	}
	if int(payload[0])*8 < alt {
		return false
	}
	i := 1 + alt/8
	if i >= len(payload) {
		return false
	}
	return payload[i]&(1<<(uint(alt)%8)) != 0
}

// Best names the widest format the device offers, for a UI that defaults to "as
// good as it gets".
func (d *Device) Best() (iface, alt, rate, bits, ch int) {
	for _, s := range d.Streams {
		for _, f := range s.Formats {
			if f.Alt == 0 || f.Subslot == 0 || len(f.Rates) == 0 {
				continue
			}
			r := f.Rates[len(f.Rates)-1]
			if f.Bits > bits || (f.Bits == bits && r > rate) {
				iface, alt, rate, bits, ch = s.Interface, f.Alt, r, f.Bits, f.Channels
			}
		}
	}
	return
}

// Caps summarizes what the terminal can claim bit-perfectly, in the shape the
// ZAT HELLO message uses.
func (d *Device) Caps() map[string]any {
	var rates, bits []int
	ch := 0
	for _, s := range d.Streams {
		for _, f := range s.Formats {
			rates = append(rates, f.Rates...)
			if f.Bits > 0 {
				bits = append(bits, f.Bits)
			}
			if f.Channels > ch {
				ch = f.Channels
			}
		}
	}
	sort.Ints(rates)
	sort.Ints(bits)
	return map[string]any{"spec": d.Spec, "speed": d.Speed, "rates": dedupe(rates), "bits": dedupe(bits), "channels": ch}
}

// rates returns the probed GET_RANGE table when the caller supplied one, since a
// live device outranks whatever its descriptors claimed at parse time.
func (o Observation) rates(s *Stream) []int {
	if o.RangeHex != "" {
		if b, err := FromHex(o.RangeHex); err == nil {
			if rs, err := DecodeRates(b); err == nil && len(rs) > 0 {
				return rs
			}
		}
	}
	for _, f := range s.Formats {
		if len(f.Rates) > 0 {
			return f.Rates
		}
	}
	return nil
}

// DecodeRates turns a UAC2 GET_RANGE payload into a rate list: a 16-bit subrange
// count followed by that many (min, max, resolution) little-endian triplets in
// Hz. A subrange whose min equals max, or whose resolution is zero, is one rate.
func DecodeRates(payload []byte) ([]int, error) {
	if len(payload) < 2 {
		return nil, errors.New("usb: short GET_RANGE payload")
	}
	n := int(le16(payload, 0))
	if 2+12*n > len(payload) {
		n = (len(payload) - 2) / 12
	}
	var out []int
	for i := 0; i < n; i++ {
		off := 2 + 12*i
		lo, hi, res := int(le32(payload, off)), int(le32(payload, off+4)), int(le32(payload, off+8))
		switch {
		case lo == 0:
		case lo == hi || res == 0 || hi < lo:
			out = append(out, lo)
		case res == 1:
			// A resolution of 1 Hz declares a continuous range; only
			// the audio rates matter, so list the standard ones the
			// range covers instead of every hertz in it.
			for _, r := range standardRates {
				if r >= lo && r <= hi {
					out = append(out, r)
				}
			}
		default:
			for r := lo; r <= hi && r-lo <= 8_000_000; r += res {
				out = append(out, r)
			}
		}
	}
	sort.Ints(out)
	return dedupe(out), nil
}

// FromHex accepts descriptor blobs as they cross the JNI boundary, ignoring the
// separators a UI may have added.
func FromHex(s string) ([]byte, error) {
	b, err := hex.DecodeString(strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t', ',', ':':
			return -1
		}
		return r
	}, s))
	if err != nil {
		return nil, fmt.Errorf("usb: hex blob: %w", err)
	}
	return b, nil
}

func specFromBCD(bcd int) string {
	switch {
	case bcd >= 0x0300:
		return "uac3"
	case bcd >= 0x0200:
		return "uac2"
	case bcd >= 0x0100:
		return "uac1"
	}
	return ""
}

func specFromProtocol(proto int) string {
	if proto == 0x20 {
		return "uac2"
	}
	return "uac1"
}

// speedName maps the device descriptor's endpoint-zero packet size and BCD to the
// frame grid the isochronous pump must use.
func speedName(dev []byte) string {
	if dev[7] == 8 {
		return "full"
	}
	if le16(dev, 12) >= 0x0300 {
		return "super"
	}
	return "high"
}

func syncName(b int) string {
	switch b {
	case 0:
		return "none"
	case 1:
		return "async"
	case 2:
		return "adaptive"
	default:
		return "vendor"
	}
}

func clockName(b int) string {
	switch b {
	case 0:
		return "external"
	case 1:
		return "internal-fixed"
	case 2:
		return "internal-variable"
	default:
		return "internal-programmable"
	}
}

func le16(b []byte, i int) uint16 { return binary.LittleEndian.Uint16(b[i:]) }
func le32(b []byte, i int) uint32 { return binary.LittleEndian.Uint32(b[i:]) }
func uac24(b []byte, i int) uint32 {
	return uint32(b[i]) | uint32(b[i+1])<<8 | uint32(b[i+2])<<16
}

func contains(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func dedupe(xs []int) []int {
	var out []int
	for i, x := range xs {
		if i == 0 || xs[i-1] != x {
			out = append(out, x)
		}
	}
	return out
}
