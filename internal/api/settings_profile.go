package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"unicode/utf8"

	"hifihub/server/internal/library"
)

// Handlers retry initialization so a failed startup write cannot enable setup.
func (a *API) initializeSettingsProfile() error {
	if a.admin == nil {
		return errors.New("settings administrator unavailable")
	}
	a.admin.mu.Lock()
	defer a.admin.mu.Unlock()
	_, err := a.settingsProfileLocked()
	return err
}

// All helpers ending in Locked require admin.mu, also held throughout setup.
func (a *API) settingsProfileLocked() (library.SettingsProfile, error) {
	s := a.admin
	if a.lib != nil {
		if s.profileInitialized {
			return a.lib.GetSettingsProfile()
		}
		p, err := a.lib.InitializeSettingsProfile(s.configured)
		if err == nil {
			s.profileInitialized = true
		}
		return p, err
	}
	// APIs without a library have no profile persistence; storage failures never use this path.
	if s.profile == nil {
		p := library.InitialSettingsProfile(s.configured)
		s.profile = &p
	}
	return *s.profile, nil
}

func (a *API) completeSettingsSetupLocked(name string) error {
	if a.lib != nil {
		_, err := a.lib.CompleteSettingsSetup(name)
		return err
	}
	p, err := a.admin.profile.WithDisplayName(name)
	if err != nil {
		return err
	}
	p.OnboardingStep, p.OnboardingComplete, p.Legacy = 2, false, false
	a.admin.profile = &p
	return nil
}

// Reject invalid UTF-8 before decoding can silently replace it with U+FFFD.
func decodeSettingsProfileRequest(w http.ResponseWriter, r *http.Request, out any, fields ...string) error {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
	if err != nil || !utf8.Valid(data) {
		return errors.New("invalid profile JSON")
	}
	// DisallowUnknownFields alone accepts duplicate and case-insensitive keys.
	// These small mutation documents require exact, unambiguous field names.
	remaining := make(map[string]bool, len(fields))
	for _, field := range fields {
		remaining[field] = true
	}
	d := json.NewDecoder(bytes.NewReader(data))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return errors.New("expected profile JSON object")
	}
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || !remaining[key] {
			return errors.New("unknown or duplicate profile field")
		}
		delete(remaining, key)
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return errors.New("invalid profile field")
		}
	}
	return decodeSettingsJSON(bytes.NewReader(data), out)
}

// The greeting is public, independently of session status; only mutations need
// the HTTPS/localhost, same-origin, administrator checks.
func (a *API) getSettingsProfile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if a.admin == nil {
		writeError(w, http.StatusServiceUnavailable, "管理员资料不可用")
		return
	}
	a.admin.mu.Lock()
	defer a.admin.mu.Unlock()
	p, err := a.settingsProfileLocked()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "无法读取管理员资料")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *API) updateSettingsProfile(w http.ResponseWriter, r *http.Request) {
	a.requireSettingsAdmin(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			DisplayName *string `json:"display_name"`
		}
		if err := decodeSettingsProfileRequest(w, r, &req, "display_name"); err != nil || req.DisplayName == nil {
			writeError(w, http.StatusBadRequest, "需要 display_name")
			return
		}
		name, err := library.NormalizeSettingsDisplayName(*req.DisplayName)
		if err != nil {
			writeError(w, http.StatusBadRequest, "显示名字须为1至40个Unicode字符，且不含控制字符")
			return
		}
		a.admin.mu.Lock()
		defer a.admin.mu.Unlock()
		p, err := a.settingsProfileLocked()
		if err == nil {
			if a.lib != nil {
				p, err = a.lib.UpdateSettingsDisplayName(name)
			} else {
				p, err = p.WithDisplayName(name)
				if err == nil {
					a.admin.profile = &p
				}
			}
		}
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "无法保存管理员资料")
			return
		}
		writeJSON(w, http.StatusOK, p)
	})(w, r)
}

func (a *API) updateSettingsOnboarding(w http.ResponseWriter, r *http.Request) {
	a.requireSettingsAdmin(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Step     *int  `json:"step"`
			Complete *bool `json:"complete"`
		}
		if err := decodeSettingsProfileRequest(w, r, &req, "step", "complete"); err != nil || req.Step == nil || req.Complete == nil || *req.Step < 1 || *req.Step > 4 || (*req.Complete && *req.Step != 4) {
			writeError(w, http.StatusBadRequest, "需要1至4的 step 和 complete，且只能在第4步完成引导")
			return
		}
		a.admin.mu.Lock()
		defer a.admin.mu.Unlock()
		p, err := a.settingsProfileLocked()
		if err == nil {
			if a.lib != nil {
				p, err = a.lib.UpdateSettingsOnboarding(*req.Step, *req.Complete)
			} else {
				p, err = p.WithOnboarding(*req.Step, *req.Complete)
				if err == nil {
					a.admin.profile = &p
				}
			}
		}
		if errors.Is(err, library.ErrSettingsOnboarding) {
			writeError(w, http.StatusBadRequest, "请先设置合法的显示名字")
			return
		}
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "无法保存引导进度")
			return
		}
		writeJSON(w, http.StatusOK, p)
	})(w, r)
}
