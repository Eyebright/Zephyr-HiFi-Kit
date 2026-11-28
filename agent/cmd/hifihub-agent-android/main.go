//go:build android

// Command hifihub-agent-android is the Android terminal core, built as a C-shared
// library (libzatagent.so) with the NDK toolchain. The Kotlin shell loads it via
// JNI and drives it through the exported Zat* functions. Audio is rendered by the
// AAudio backend (player package, cgo && android); the pure-Go ZAT sink, config,
// state and server client are shared with the Linux agent.
package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"encoding/json"
	"sync"
	"time"
	"unsafe"

	"hifihub/agent/internal/androidcore"
	"hifihub/agent/internal/discover"
)

var (
	mu     sync.Mutex
	core   *androidcore.Core
	cancel context.CancelFunc
)

func cstr(s string) *C.char { return C.CString(s) }

func jsonResult(v any) *C.char {
	b, err := json.Marshal(v)
	if err != nil {
		return cstr(`{"error":"marshal"}`)
	}
	return cstr(string(b))
}

// ZatStart parses the JSON config, builds the core and launches the ZAT sink and
// local console. It returns JSON {"console_url":...} or {"error":...}. The caller
// must free the returned string with ZatFree.
//
//export ZatStart
func ZatStart(cfgJSON *C.char) *C.char {
	mu.Lock()
	defer mu.Unlock()
	if cancel != nil {
		cancel()
		cancel = nil
		core = nil
	}
	var in androidcore.Config
	if err := json.Unmarshal([]byte(C.GoString(cfgJSON)), &in); err != nil {
		return jsonResult(map[string]string{"error": "config: " + err.Error()})
	}
	c, err := androidcore.New(in)
	if err != nil {
		return jsonResult(map[string]string{"error": err.Error()})
	}
	ctx, cx := context.WithCancel(context.Background())
	core, cancel = c, cx
	go c.Run(ctx)
	go func() { _ = androidcore.ServeConsole(ctx, c, c.ConsoleAddr()) }()
	return jsonResult(map[string]string{"console_url": "http://" + c.ConsoleAddr()})
}

// ZatStop cancels the running core (ZAT sink + console).
//
//export ZatStop
func ZatStop() {
	mu.Lock()
	defer mu.Unlock()
	if cancel != nil {
		cancel()
		cancel = nil
		core = nil
	}
}

// ZatDiscoverServer scans the local /24 for the management server's health
// endpoint so first pairing only needs the registration code. It returns
// {"server_url":...} or {"error":...}.
//
//export ZatDiscoverServer
func ZatDiscoverServer() *C.char {
	ctx, cx := context.WithTimeout(context.Background(), 20*time.Second)
	defer cx()
	u, ok := discover.FindServer(ctx, 8080)
	if !ok {
		return jsonResult(map[string]string{"error": "局域网内未发现服务端"})
	}
	return jsonResult(map[string]string{"server_url": u})
}

func withCore() *androidcore.Core {
	mu.Lock()
	defer mu.Unlock()
	return core
}

// ZatState returns JSON {identity, playback, metrics} for the native UI.
//
//export ZatState
func ZatState() *C.char {
	c := withCore()
	if c == nil {
		return jsonResult(map[string]string{"error": "not started"})
	}
	ctx, cx := context.WithTimeout(context.Background(), 3*time.Second)
	defer cx()
	return jsonResult(map[string]any{
		"identity": c.Identity(ctx),
		"playback": c.Playback(),
		"metrics":  c.Metrics(),
	})
}

// ZatPause toggles pause/resume, returning {"paused":bool}.
//
//export ZatPause
func ZatPause() *C.char {
	c := withCore()
	if c == nil {
		return jsonResult(map[string]string{"error": "not started"})
	}
	paused, err := c.Pause()
	if err != nil {
		return jsonResult(map[string]string{"error": err.Error()})
	}
	return jsonResult(map[string]bool{"paused": paused})
}

// ZatQueueNext advances the server-owned playback queue.
//
//export ZatQueueNext
func ZatQueueNext() *C.char {
	c := withCore()
	if c == nil {
		return jsonResult(map[string]string{"error": "not started"})
	}
	ctx, cx := context.WithTimeout(context.Background(), 10*time.Second)
	defer cx()
	if err := c.PlaybackNext(ctx); err != nil {
		return jsonResult(map[string]string{"error": err.Error()})
	}
	return jsonResult(map[string]bool{"ok": true})
}

// ZatQueuePrev returns to the previous server queue entry.
//
//export ZatQueuePrev
func ZatQueuePrev() *C.char {
	c := withCore()
	if c == nil {
		return jsonResult(map[string]string{"error": "not started"})
	}
	ctx, cx := context.WithTimeout(context.Background(), 10*time.Second)
	defer cx()
	if err := c.PlaybackPrev(ctx); err != nil {
		return jsonResult(map[string]string{"error": err.Error()})
	}
	return jsonResult(map[string]bool{"ok": true})
}

// ZatStopPlayback drains AAudio and tears down the ZAT session.
//
//export ZatStopPlayback
func ZatStopPlayback() *C.char {
	c := withCore()
	if c == nil {
		return jsonResult(map[string]string{"error": "not started"})
	}
	if err := c.Stop(); err != nil {
		return jsonResult(map[string]string{"error": err.Error()})
	}
	return jsonResult(map[string]bool{"ok": true})
}

// ZatSeek skips to a sample position.
//
//export ZatSeek
func ZatSeek(pts C.ulonglong) *C.char {
	c := withCore()
	if c == nil {
		return jsonResult(map[string]string{"error": "not started"})
	}
	if err := c.Seek(uint64(pts)); err != nil {
		return jsonResult(map[string]string{"error": err.Error()})
	}
	return jsonResult(map[string]bool{"ok": true})
}

// ZatVolume sets soft volume 0..100.
//
//export ZatVolume
func ZatVolume(v C.int) *C.char {
	c := withCore()
	if c == nil {
		return jsonResult(map[string]string{"error": "not started"})
	}
	if err := c.Volume(int(v)); err != nil {
		return jsonResult(map[string]string{"error": err.Error()})
	}
	return jsonResult(map[string]bool{"ok": true})
}

// ZatUsbProbe decodes the hex device and configuration descriptor blobs Kotlin
// read from a USB DAC and returns its capability JSON, or {"error":...}. The
// descriptors cross as hex because only the Java USB host API can read them.
//
//export ZatUsbProbe
func ZatUsbProbe(devHex, cfgHex, product *C.char) *C.char {
	return cstr(androidcore.UsbProbe(C.GoString(devHex), C.GoString(cfgHex), C.GoString(product)))
}

// ZatUsbRates decodes a GET_RANGE reply into the sample rates a UAC2 clock
// accepts, as {"rates":[...]} or {"error":...}.
//
//export ZatUsbRates
func ZatUsbRates(rangeHex *C.char) *C.char {
	return cstr(androidcore.UsbRates(C.GoString(rangeHex)))
}

// ZatUsbPlan returns the transfers and isochronous sizing that put one stream
// into a format, from the hex blobs plus a usb.Observation JSON.
//
//export ZatUsbPlan
func ZatUsbPlan(devHex, cfgHex, obsJSON *C.char) *C.char {
	return cstr(androidcore.UsbPlan(C.GoString(devHex), C.GoString(cfgHex), C.GoString(obsJSON)))
}

// ZatUsbValidate renders the request that asks a UAC2 device whether an
// alternate setting is usable; replyHex, when given, also reports "supported".
//
//export ZatUsbValidate
func ZatUsbValidate(iface, alt C.int, replyHex *C.char) *C.char {
	return cstr(androidcore.UsbValidate(int(iface), int(alt), C.GoString(replyHex)))
}

// ZatUsbToneStart runs the exclusive-output prototype: Kotlin has already
// claimed the interface and set the alternate setting, reqJSON names the
// descriptor from getFileDescriptor(), the endpoint and the negotiated format,
// and the Go side drives isochronous URBs past the Android mixer.
//
//export ZatUsbToneStart
func ZatUsbToneStart(reqJSON *C.char) *C.char {
	return cstr(androidcore.UsbToneStart(C.GoString(reqJSON)))
}

// ZatUsbToneStat reports the run in progress or the one that finished.
//
//export ZatUsbToneStat
func ZatUsbToneStat() *C.char {
	return cstr(androidcore.UsbToneStat())
}

// ZatUsbToneStop ends the run and returns the verdict. Kotlin releases the
// interface afterwards, which rebinds the kernel driver and restores AAudio.
//
//export ZatUsbToneStop
func ZatUsbToneStop() *C.char {
	return cstr(androidcore.UsbToneStop())
}

// ZatFree releases a string returned by any Zat* function.
//
//export ZatFree
func ZatFree(p *C.char) { C.free(unsafe.Pointer(p)) }

func main() {}
