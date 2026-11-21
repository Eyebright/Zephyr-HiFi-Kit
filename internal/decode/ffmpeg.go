package decode

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"hifihub/zat"
)

// ffmpegDecoder streams PCM produced by the ffmpeg CLI. It is the fallback for
// DSD files when the terminal cannot take DoP; the server still owns decoding,
// so terminals stay PCM-only.
type ffmpegDecoder struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr *bytes.Buffer
	format PCMFormat
	total  uint64
	once   sync.Once
}

// FFmpegAvailable reports whether an ffmpeg executable is usable.
func FFmpegAvailable() bool {
	_, err := exec.LookPath(ffmpegPath())
	return err == nil
}

func ffmpegPath() string {
	if p := os.Getenv("HIFIHUB_FFMPEG"); p != "" {
		return p
	}
	if p := envFFmpeg; p != "" {
		return p
	}
	return "ffmpeg"
}

// envFFmpeg is overridable in tests.
var envFFmpeg = ""

// OpenFFmpegPCM converts path to interleaved S24 PCM at rate/channels.
func OpenFFmpegPCM(path string, rate, channels, durationMS int) (Decoder, error) {
	if rate <= 0 || channels <= 0 {
		return nil, fmt.Errorf("decode: invalid ffmpeg target %d Hz/%d ch", rate, channels)
	}
	if !FFmpegAvailable() {
		return nil, fmt.Errorf("decode: ffmpeg not found for DSD PCM fallback")
	}
	cmd := exec.Command(ffmpegPath(),
		"-nostdin", "-hide_banner", "-v", "error",
		"-i", path, "-map", "0:a:0",
		"-f", "s24le", "-acodec", "pcm_s24le",
		"-ar", fmt.Sprint(rate), "-ac", fmt.Sprint(channels), "pipe:1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	total := uint64(0)
	if durationMS > 0 {
		total = uint64(durationMS) * uint64(rate) / 1000
	}
	return &ffmpegDecoder{cmd: cmd, stdout: stdout, stderr: stderr, total: total,
		format: PCMFormat{Format: zat.S24LE, Rate: rate, Channels: channels}}, nil
}

func (d *ffmpegDecoder) Format() PCMFormat    { return d.format }
func (d *ffmpegDecoder) TotalSamples() uint64 { return d.total }

func (d *ffmpegDecoder) Read(p []byte) (int, error) {
	n, err := d.stdout.Read(p)
	if err == io.EOF && n == 0 {
		if werr := d.cmd.Wait(); werr != nil {
			msg := bytes.TrimSpace(d.stderr.Bytes())
			if len(msg) > 0 {
				return 0, fmt.Errorf("decode: ffmpeg: %s", msg)
			}
			return 0, fmt.Errorf("decode: ffmpeg: %w", werr)
		}
	}
	return n, err
}

func (d *ffmpegDecoder) Close() error {
	d.once.Do(func() {
		if d.stdout != nil {
			_ = d.stdout.Close()
		}
		if d.cmd != nil && d.cmd.Process != nil {
			_ = d.cmd.Process.Kill()
			_ = d.cmd.Wait()
		}
	})
	return nil
}
