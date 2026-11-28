package androidcore

// USB exclusive output: the Kotlin shell owns the Android USB host API and hands
// over raw descriptor blobs, this file turns them into the JSON answers the JNI
// boundary passes as strings. The descriptor arithmetic itself lives in
// internal/usb, which is why these wrappers are thin enough to leave untested by
// hardware.

import (
	"encoding/json"
	"errors"

	"hifihub/agent/internal/usb"
)

// UsbProbe decodes the device and configuration descriptor blobs of a DAC and
// returns its capability model: {"spec","speed","streams","clocks"} or
// {"error":...}. product is the name Android already knows, echoed for the UI.
func UsbProbe(devHex, cfgHex, product string) string {
	d, err := parseUSB(devHex, cfgHex, product)
	if err != nil {
		return usbError(err)
	}
	return usbJSON(d)
}

// UsbRates decodes the payload of a stream's GET_RANGE reply into the sample
// rates the device actually accepts: {"rates":[...]} or {"error":...}.
func UsbRates(rangeHex string) string {
	raw, err := usb.FromHex(rangeHex)
	if err != nil {
		return usbError(err)
	}
	rates, err := usb.DecodeRates(raw)
	if err != nil {
		return usbError(err)
	}
	if len(rates) == 0 {
		return usbError(errors.New("GET_RANGE reply listed no sample rates"))
	}
	return usbJSON(map[string]any{"rates": rates})
}

// UsbPlan returns the transfers and isochronous sizing that put one stream into
// a format: {"alt","requests","nominalBytes",...} or {"error":...}. obsJSON is a
// usb.Observation, whose rangeHex carries the GET_RANGE reply read for that
// stream when the descriptors did not list the rates.
func UsbPlan(devHex, cfgHex, obsJSON string) string {
	var obs usb.Observation
	if err := json.Unmarshal([]byte(obsJSON), &obs); err != nil {
		return usbError(err)
	}
	d, err := parseUSB(devHex, cfgHex, "")
	if err != nil {
		return usbError(err)
	}
	plan, err := usb.Negotiate(d, obs)
	if err != nil {
		return usbError(err)
	}
	return usbJSON(plan)
}

// UsbValidate renders the request that asks a UAC2 device whether an alternate
// setting is usable, and decodes the reply Kotlin passed back in replyHex.
func UsbValidate(iface, alt int, replyHex string) string {
	req := usb.ValidateAltRequest(iface, alt)
	if replyHex == "" {
		return usbJSON(req)
	}
	raw, err := usb.FromHex(replyHex)
	if err != nil {
		return usbError(err)
	}
	return usbJSON(map[string]any{"request": req, "supported": usb.AltSupported(raw, alt)})
}

func parseUSB(devHex, cfgHex, product string) (*usb.Device, error) {
	dev, err := usb.FromHex(devHex)
	if err != nil {
		return nil, err
	}
	cfg, err := usb.FromHex(cfgHex)
	if err != nil {
		return nil, err
	}
	return usb.Parse(dev, cfg, product)
}

func usbJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return usbError(err)
	}
	return string(b)
}

func usbError(err error) string {
	b, _ := json.Marshal(map[string]string{"error": err.Error()})
	return string(b)
}
