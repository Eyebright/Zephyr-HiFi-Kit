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

// ZatFree releases a string returned by any Zat* function.
//
//export ZatFree
func ZatFree(p *C.char) { C.free(unsafe.Pointer(p)) }

func main() {}
