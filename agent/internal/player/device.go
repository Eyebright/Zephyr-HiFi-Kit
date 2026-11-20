package player

import "strings"

// plugDevice maps a strict hardware PCM name to ALSA's plug variant so the
// device can convert sample formats it does not accept natively.
func plugDevice(device string) (string, bool) {
	if strings.HasPrefix(device, "hw:") {
		return "plughw:" + strings.TrimPrefix(device, "hw:"), true
	}
	return "", false
}
