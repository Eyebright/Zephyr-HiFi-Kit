package decode

import (
	"io"
	"testing"
)

func TestFLACDecodesToPCM(t *testing.T) {
	d, err := Open("testdata/silence.flac")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	f := d.Format()
	if f.Rate <= 0 || f.Channels <= 0 || f.BytesPerFrame() <= 0 {
		t.Fatalf("format=%+v", f)
	}
	buf := make([]byte, 8192)
	var bytesRead uint64
	for {
		n, err := d.Read(buf)
		bytesRead += uint64(n)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if bytesRead == 0 || bytesRead%uint64(f.BytesPerFrame()) != 0 {
		t.Fatalf("decoded %d bytes, frame size %d", bytesRead, f.BytesPerFrame())
	}
	if total := d.TotalSamples(); total > 0 && bytesRead/uint64(f.BytesPerFrame()) != total {
		t.Fatalf("decoded samples=%d total=%d", bytesRead/uint64(f.BytesPerFrame()), total)
	}
}
