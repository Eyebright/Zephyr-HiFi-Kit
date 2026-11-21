package player

// pack24to32 widens packed 3-byte samples to 4 bytes with the 24 payload bits
// left-aligned and the low byte zeroed.
// pack16to32 widens 16-bit samples to 32-bit with the payload left-aligned.
func pack16to32(pcm []byte) []byte {
	out := make([]byte, 0, len(pcm)/2*4)
	for i := 0; i+2 <= len(pcm); i += 2 {
		out = append(out, 0, 0, pcm[i], pcm[i+1])
	}
	return out
}

func pack24to32(pcm []byte) []byte {
	out := make([]byte, 0, len(pcm)/3*4)
	for i := 0; i+3 <= len(pcm); i += 3 {
		out = append(out, 0, pcm[i], pcm[i+1], pcm[i+2])
	}
	return out
}
