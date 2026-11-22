package api

import (
	"io"
	"net/http"
	"strings"

	"hifihub/server/internal/domain"
)

func (a *API) listApps(w http.ResponseWriter, _ *http.Request) {
	apps, err := a.cat.ListApps()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apps)
}

func (a *API) createApp(w http.ResponseWriter, r *http.Request) {
	var app domain.App
	if err := decode(r, &app); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	app, err := a.cat.CreateApp(app)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, app)
}

func (a *API) getApp(w http.ResponseWriter, r *http.Request) {
	app, err := a.cat.GetApp(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, app)
}

// updateApp reclassifies how an app is obtained (public vs self-supplied).
func (a *API) updateApp(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Origin domain.AppOrigin `json:"origin"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	app, err := a.cat.SetAppOrigin(r.PathValue("id"), body.Origin)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, app)
}

func (a *API) listVersions(w http.ResponseWriter, r *http.Request) {
	vs, err := a.cat.ListVersions(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, vs)
}

// addVersion accepts either JSON metadata alone, or multipart/form-data with a
// "meta" JSON field plus an "artifact" file and optional "signature" (.asc).
func (a *API) addVersion(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	var v domain.AppVersion

	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_multipart", err.Error(), "")
			return
		}
		if err := jsonUnmarshalString(r.FormValue("meta"), &v); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_meta", err.Error(), "")
			return
		}
		file, header, err := r.FormFile("artifact")
		if err != nil {
			writeErr(w, http.StatusBadRequest, "missing_artifact", "multipart field 'artifact' required", "")
			return
		}
		defer file.Close()

		var sig []byte
		if sf, _, err := r.FormFile("signature"); err == nil {
			sig, _ = io.ReadAll(sf)
			sf.Close()
		}
		res, err := a.repo.Put(appID, v.Version, v.Arch, header.Filename, file, sig)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "artifact_rejected", err.Error(), "")
			return
		}
		v.SHA256, v.Size, v.ArtifactPath, v.Signed = res.SHA256, res.Size, res.RelPath, res.Signed
		if res.SigPath != "" {
			v.Signature = res.SigPath
		}
	} else {
		if err := decode(r, &v); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
			return
		}
	}

	v.AppID = appID
	v, err := a.cat.AddVersion(v)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (a *API) banVersion(w http.ResponseWriter, r *http.Request) {
	v, err := a.cat.GetVersion(r.PathValue("id"), r.PathValue("ver"), r.PathValue("arch"))
	if err != nil {
		fail(w, err)
		return
	}
	var body struct {
		Banned bool `json:"banned"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	if err := a.cat.SetBanned(v.ID, body.Banned); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": v.ID, "banned": body.Banned})
}

func (a *API) lifecycleVersion(w http.ResponseWriter, r *http.Request) {
	v, err := a.cat.GetVersion(r.PathValue("id"), r.PathValue("ver"), r.PathValue("arch"))
	if err != nil {
		fail(w, err)
		return
	}
	var body struct {
		Lifecycle string `json:"lifecycle"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	if err := a.cat.SetLifecycle(v.ID, body.Lifecycle); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": v.ID, "lifecycle": body.Lifecycle})
}
