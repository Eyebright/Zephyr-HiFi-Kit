package androidcore

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// uac2Device is the 18-byte device descriptor of a high-speed audio class 2
// device: bcdUSB 2.0 and a 64-byte endpoint-zero packet.
var uac2Device = hex.EncodeToString([]byte{
	18, 0x01, 0x00, 0x02, 0x00, 0x00, 0x00, 64,
	0x6d, 0x2a, 0x00, 0x08, 0x00, 0x01, 1, 2, 3, 1,
})

// uac2Blob is a real if minimal UAC2 output function: one writable clock, a
// feature-free terminal graph, and a streaming interface whose alternate 1 is
// 32-bit stereo behind an asynchronous isochronous endpoint. Rates are absent by
// design; they live in the clock and must be probed.
func uac2Blob() string {
	b := []byte{
		9, 0x02, 0, 0, 2, 1, 0, 0xc0, 250, // configuration header
		9, 0x04, 0, 0, 0, 0x01, 0x01, 0x20, 0, // AudioControl interface 0
		9, 0x24, 0x01, 0x00, 0x02, 0x01, 0x3b, 0x00, 0x00, // header, bcdADC 2.0
		8, 0x24, 0x0a, 0x28, 0x07, 0x03, 0x00, 0x00, // clock 0x28: internal programmable, synced to SOF, read+write frequency
		14, 0x24, 0x02, 0x01, 0x01, 0x01, 0x00, 0x28, 0x02, 0x03, 0x00, 0x00, 0x00, 0x00, // input terminal 1, 2 ch
		11, 0x24, 0x03, 0x02, 0x01, 0x03, 0x00, 0x01, 0x28, 0x00, 0x00, // output terminal 2 ← unit 1, clock 0x28
		9, 0x04, 1, 1, 1, 0x01, 0x02, 0x20, 0, // AudioStreaming interface 1, alternate 1
		16, 0x24, 0x01, 0x02, 0x00, 0x01, 0x07, 0x00, 0x00, 0x00, 0x02, 0x03, 0x00, 0x00, 0x00, 0x00, // AS_GENERAL → terminal 2, 2 ch
		6, 0x24, 0x02, 0x01, 0x04, 0x20, // FORMAT_TYPE_I: 4-byte subslot, 32 bits
		9, 0x05, 0x01, 0x05, 0x38, 0x04, 1, 0x06, 0x82, // isoc async, 1080 B, bRefresh 6, feedback endpoint 0x82
	}
	b[2], b[3] = byte(len(b)), byte(len(b)>>8)
	return hex.EncodeToString(b)
}

// rateRangePayload encodes the given rates as GET_RANGE single-value triplets.
func rateRangePayload(rates ...int) string {
	b := []byte{byte(len(rates)), 0}
	for _, r := range rates {
		for i := 0; i < 2; i++ {
			b = append(b, byte(r), byte(r>>8), byte(r>>16), byte(r>>24))
		}
		b = append(b, 0, 0, 0, 0)
	}
	return hex.EncodeToString(b)
}

func TestUsbProbeJSON(t *testing.T) {
	out := UsbProbe(uac2Device, uac2Blob(), "Test DAC")
	var got struct {
		Spec    string `json:"spec"`
		Speed   string `json:"speed"`
		Product string `json:"product"`
		Streams []struct {
			Interface   int `json:"interface"`
			ClockID     int `json:"clockId"`
			RateRequest *struct {
				BMRequestType int `json:"bmRequestType"`
				BRequest      int `json:"bRequest"`
				WValue        int `json:"wValue"`
				WIndex        int `json:"wIndex"`
			} `json:"rateRequest"`
			Endpoint struct {
				Sync            string `json:"sync"`
				MaxPacket       int    `json:"maxPacket"`
				FeedbackAddress int    `json:"feedbackAddress"`
			} `json:"endpoint"`
		} `json:"streams"`
		Clocks []struct {
			Type        string `json:"type"`
			FreqControl bool   `json:"freqControl"`
			SyncedToSof bool   `json:"syncedToSof"`
		} `json:"clocks"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if got.Spec != "uac2" || got.Speed != "high" || got.Product != "Test DAC" {
		t.Errorf("header = %q/%q/%q", got.Spec, got.Speed, got.Product)
	}
	if len(got.Streams) != 1 {
		t.Fatalf("streams = %s", out)
	}
	s := got.Streams[0]
	if s.Interface != 1 || s.ClockID != 0x28 {
		t.Errorf("stream = %+v", s)
	}
	if s.Endpoint.Sync != "async" || s.Endpoint.MaxPacket != 1080 || s.Endpoint.FeedbackAddress != 0x82 {
		t.Errorf("endpoint = %+v", s.Endpoint)
	}
	if s.RateRequest == nil {
		t.Fatal("no rate request for a UAC2 stream")
	}
	if s.RateRequest.BMRequestType != 0xa1 || s.RateRequest.BRequest != 0x02 ||
		s.RateRequest.WValue != 0x0100 || s.RateRequest.WIndex != 0x2800 {
		t.Errorf("rate request = %+v, want 0xa1/0x02/0x0100/0x2800", s.RateRequest)
	}
	if len(got.Clocks) != 1 || got.Clocks[0].Type != "internal-programmable" ||
		!got.Clocks[0].FreqControl || !got.Clocks[0].SyncedToSof {
		t.Errorf("clocks = %+v", got.Clocks)
	}
}

func TestUsbProbeErrors(t *testing.T) {
	for _, tc := range []struct{ name, dev, cfg, want string }{
		{"bad config hex", uac2Device, "zz", "hex blob"},
		{"not an audio device", uac2Device,
			hex.EncodeToString([]byte{9, 0x02, 9, 0, 1, 1, 0, 0xc0, 50}), "unsupported"},
		{"short device blob", "aabb", uac2Blob(), "unsupported"},
		{"bad device hex", "zz", uac2Blob(), "hex blob"},
	} {
		if out := UsbProbe(tc.dev, tc.cfg, "x"); !strings.Contains(out, `"error"`) || !strings.Contains(out, tc.want) {
			t.Errorf("%s: %s, want an error naming %q", tc.name, out, tc.want)
		}
	}
}

func TestUsbRates(t *testing.T) {
	if out := UsbRates(rateRangePayload(44100, 192000)); !strings.Contains(out, `"rates":[44100,192000]`) {
		t.Errorf("UsbRates = %s", out)
	}
	if out := UsbRates("nothex"); !strings.Contains(out, "hex blob") {
		t.Errorf("bad hex = %s", out)
	}
	if out := UsbRates("0100"); !strings.Contains(out, `"error"`) {
		t.Errorf("short payload = %s, want an error", out)
	}
}

func TestUsbPlan(t *testing.T) {
	// Without the probed table the plan must report the request it still needs.
	out := UsbPlan(uac2Device, uac2Blob(), `{"interface":1,"rate":192000,"bits":32}`)
	if !strings.Contains(out, "no rates yet") {
		t.Errorf("unprobed plan = %s", out)
	}
	obs := `{"interface":1,"rate":192000,"bits":32,"rangeHex":"` + rateRangePayload(44100, 192000) + `"}`
	var plan struct {
		Alt            int      `json:"alt"`
		Rate           int      `json:"rate"`
		SampleBytes    int      `json:"sampleBytes"`
		NominalBytes   int      `json:"nominalBytes"`
		ClockID        int      `json:"clockId"`
		MaxPacketBytes int      `json:"maxPacketBytes"`
		Error          string   `json:"error"`
		Notes          []string `json:"notes"`
		Requests       []struct {
			BMRequestType int    `json:"bmRequestType"`
			WValue        int    `json:"wValue"`
			WIndex        int    `json:"wIndex"`
			Data          string `json:"data"`
		} `json:"requests"`
	}
	if err := json.Unmarshal([]byte(UsbPlan(uac2Device, uac2Blob(), obs)), &plan); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if plan.Error != "" {
		t.Fatalf("plan error: %s", plan.Error)
	}
	if plan.Alt != 1 || plan.Rate != 192000 || plan.SampleBytes != 8 || plan.ClockID != 0x28 {
		t.Errorf("plan = %+v", plan)
	}
	// 192 kHz x 8 B over 8000 microframes is exactly 192 B per packet.
	if plan.NominalBytes != 192 || plan.MaxPacketBytes != 1080 {
		t.Errorf("sizing = %d of %d B", plan.NominalBytes, plan.MaxPacketBytes)
	}
	if len(plan.Requests) != 1 {
		t.Fatalf("requests = %+v", plan.Requests)
	}
	r := plan.Requests[0]
	if r.BMRequestType != 0x21 || r.WValue != 0x0100 || r.WIndex != 0x2800 || r.Data != "00ee0200" {
		t.Errorf("set-rate = %+v", r)
	}

	// A rate the device never reported must be refused, and so must a malformed
	// observation.
	bad := `{"interface":1,"rate":384000,"rangeHex":"` + rateRangePayload(44100) + `"}`
	if out := UsbPlan(uac2Device, uac2Blob(), bad); !strings.Contains(out, "no alternate setting") {
		t.Errorf("unsupported rate = %s", out)
	}
	if out := UsbPlan(uac2Device, uac2Blob(), `{"interface":`); !strings.Contains(out, `"error"`) {
		t.Errorf("malformed observation = %s", out)
	}
}

func TestUsbValidate(t *testing.T) {
	out := UsbValidate(1, 2, "")
	if !strings.Contains(out, `"bmRequestType":161`) || !strings.Contains(out, `"wValue":512`) ||
		!strings.Contains(out, `"expectLen":8`) {
		t.Errorf("request only = %s, want 0xa1/0x0200/8", out)
	}
	if out := UsbValidate(1, 3, "0108"); !strings.Contains(out, `"supported":true`) {
		t.Errorf("alt 3 in bitmap 0x08 = %s", out)
	}
	if out := UsbValidate(1, 4, "0108"); !strings.Contains(out, `"supported":false`) {
		t.Errorf("alt 4 unset = %s", out)
	}
	if out := UsbValidate(1, 1, "zz"); !strings.Contains(out, "hex blob") {
		t.Errorf("bad reply = %s", out)
	}
}
