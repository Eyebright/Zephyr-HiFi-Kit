package zatserver

import (
	"encoding/binary"
	"os"
	"testing"
)

// writeTestDSF writes a minimal valid stereo DSF: 32 DSD samples per channel in
// two 4096-byte blocks (padded), enough to exercise probe, scan and DoP setup.
func writeTestDSF(t *testing.T, path string) {
	t.Helper()
	const blockSize = 4096
	h := make([]byte, 92)
	copy(h[0:4], "DSD ")
	binary.LittleEndian.PutUint64(h[4:12], 28)
	binary.LittleEndian.PutUint64(h[12:20], 0)
	copy(h[28:32], "fmt ")
	binary.LittleEndian.PutUint64(h[32:40], 52)
	binary.LittleEndian.PutUint32(h[40:44], 1)
	binary.LittleEndian.PutUint32(h[48:52], 2)
	binary.LittleEndian.PutUint32(h[52:56], 2)
	binary.LittleEndian.PutUint32(h[56:60], 2822400)
	binary.LittleEndian.PutUint32(h[60:64], 8)
	binary.LittleEndian.PutUint64(h[64:72], 32)
	binary.LittleEndian.PutUint32(h[72:76], blockSize)
	copy(h[80:84], "data")
	data := make([]byte, 2*blockSize)
	for i := 0; i < 32; i++ {
		data[i] = byte(i)
		data[blockSize+i] = byte(i + 64)
	}
	binary.LittleEndian.PutUint64(h[84:92], uint64(len(data)))
	if err := os.WriteFile(path, append(h, data...), 0o644); err != nil {
		t.Fatal(err)
	}
}
