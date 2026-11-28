package player

import (
	"encoding/binary"
	"math"

	"hifihub/zat"
)

// pcmToFloat converts integer PCM to little-endian float32 for AAudio streams
// that only accept AAUDIO_FORMAT_PCM_FLOAT. 16/24-bit integers are exactly
// representable in float32; 32-bit sources lose their lowest bits, which the
// Android mixer discards anyway on its float path.
func pcmToFloat(pcm []byte, f zat.Format) []byte {
	bps := f.BytesPerSample()
	if bps <= 0 {
		return nil
	}
	samples := len(pcm) / bps
	out := make([]byte, samples*4)
	for i := 0; i < samples; i++ {
		var v float32
		switch f {
		case zat.S16LE:
			v = float32(int16(binary.LittleEndian.Uint16(pcm[i*2:]))) / 32768
		case zat.S24LE:
			x := int32(pcm[i*3]) | int32(pcm[i*3+1])<<8 | int32(pcm[i*3+2])<<16
			if x&0x800000 != 0 {
				x -= 1 << 24
			}
			v = float32(x) / 8388608
		case zat.S32LE:
			v = float32(int32(binary.LittleEndian.Uint32(pcm[i*4:]))) / 2147483648
		default:
			return nil
		}
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(v))
	}
	return out
}