package mqa

import (
	"os"
	"testing"
)

func TestAnalyzeSamplesSynthetic(t *testing.T) {
	const pos = 18
	frames := make([]int32, 200)
	for i := 0; i < 36; i++ {
		bit := (magic >> (35 - i)) & 1
		frames[2*i+1] = int32(bit) << pos
	}
	// Original rate code 0x3 => 44.1k * 4 = 176.4k; provenance 0x1F => studio.
	setBits := func(start, n int, v int) {
		for i := 0; i < n; i++ {
			bit := (v >> (n - 1 - i)) & 1
			frames[2*(start+i)+1] |= int32(bit) << pos
		}
	}
	setBits(35+3, 4, 0x3)
	setBits(35+29, 5, 0x1F)
	r := AnalyzeSamples(frames, 0)
	if !r.Detected || !r.Studio || r.OriginalRate != 96000 || r.BitPosition != pos || r.SyncPosition != 35 {
		t.Fatalf("result = %+v", r)
	}
}

func TestAnalyzeSamplesRejectsNoise(t *testing.T) {
	frames := make([]int32, 5000)
	for i := range frames {
		frames[i] = int32(i*2654435761) & 0x00FFFFFF
	}
	if r := AnalyzeSamples(frames, 0); r.Detected {
		t.Fatalf("false positive: %+v", r)
	}
}

// TestDetectRealFile is opt-in: set MQA_TEST_FILE to an MQA-encoded file.
func TestDetectRealFile(t *testing.T) {
	path := os.Getenv("MQA_TEST_FILE")
	if path == "" {
		t.Skip("MQA_TEST_FILE not set")
	}
	r, err := DetectFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Detected {
		t.Fatalf("no MQA sync word in %s", path)
	}
	t.Logf("detected: %+v", r)
}
