package api

import (
	"net/http"

	"hifihub/server/internal/library"
)

type settingsLanguageResponse struct {
	DefaultLanguage string `json:"default_language"`
}

func (a *API) registerSettingsLanguageRoutes() {
	a.route("GET /api/v1/settings/language", a.getSettingsLanguage)
	a.route("PATCH /api/v1/settings/language", a.updateSettingsLanguage)
}

// The caller holds admin.mu; storage failures must not fall back to an in-memory value.
func (a *API) settingsLanguageLocked() (string, error) {
	if a.lib != nil {
		return a.lib.GetSettingsLanguage()
	}
	if a.admin.defaultLanguage == "" {
		return library.DefaultSettingsLanguage, nil
	}
	return a.admin.defaultLanguage, nil
}

func (a *API) getSettingsLanguage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if a.admin == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "settings_language_unavailable", "语言设置不可用")
		return
	}
	a.admin.mu.Lock()
	defer a.admin.mu.Unlock()
	language, err := a.settingsLanguageLocked()
	if err != nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "settings_language_read_failed", "无法读取语言设置")
		return
	}
	writeJSON(w, http.StatusOK, settingsLanguageResponse{DefaultLanguage: language})
}

func (a *API) updateSettingsLanguage(w http.ResponseWriter, r *http.Request) {
	a.requireSettingsAdmin(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			DefaultLanguage *string `json:"default_language"`
		}
		if err := decodeSettingsProfileRequest(w, r, &req, "default_language"); err != nil || req.DefaultLanguage == nil {
			writeErrorCode(w, http.StatusBadRequest, "settings_language_required", "需要 default_language")
			return
		}
		language := *req.DefaultLanguage
		if err := library.ValidateSettingsLanguage(language); err != nil {
			writeErrorCode(w, http.StatusBadRequest, "settings_language_invalid", "默认语言须为 zh-CN、zh-TW 或 en")
			return
		}
		a.admin.mu.Lock()
		defer a.admin.mu.Unlock()
		if a.lib != nil {
			if _, err := a.lib.UpdateSettingsLanguage(language); err != nil {
				writeErrorCode(w, http.StatusServiceUnavailable, "settings_language_save_failed", "无法保存语言设置")
				return
			}
		} else {
			a.admin.defaultLanguage = language
		}
		writeJSON(w, http.StatusOK, settingsLanguageResponse{DefaultLanguage: language})
	})(w, r)
}
