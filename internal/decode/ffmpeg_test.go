package decode

import (
	"os"
	"testing"
)

func TestFFmpegPathEnvOverride(t *testing.T) {
	t.Setenv("HIFIHUB_FFMPEG", `C:\tools\ffmpeg.exe`)
	if got := ffmpegPath(); got != `C:\tools\ffmpeg.exe` {
		t.Fatalf("ffmpegPath = %q", got)
	}
	os.Unsetenv("HIFIHUB_FFMPEG")
}
