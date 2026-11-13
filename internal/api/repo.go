package api

import (
	"net/http"
	"os"
)

// downloadArtifact serves the artifact with HTTP Range support (FR-3.5.1).
func (a *API) downloadArtifact(w http.ResponseWriter, r *http.Request) {
	v, err := a.cat.GetVersion(r.PathValue("app"), r.PathValue("ver"), r.PathValue("arch"))
	if err != nil {
		fail(w, err)
		return
	}
	if v.ArtifactPath == "" {
		writeErr(w, http.StatusNotFound, "no_artifact", "version has no stored artifact", "")
		return
	}
	path := a.repo.AbsPath(v.ArtifactPath)
	if _, err := os.Stat(path); err != nil {
		writeErr(w, http.StatusNotFound, "artifact_missing", "artifact file missing on disk", "")
		return
	}
	w.Header().Set("X-Artifact-SHA256", v.SHA256)
	http.ServeFile(w, r, path)
}

func (a *API) downloadSignature(w http.ResponseWriter, r *http.Request) {
	v, err := a.cat.GetVersion(r.PathValue("app"), r.PathValue("ver"), r.PathValue("arch"))
	if err != nil {
		fail(w, err)
		return
	}
	if v.Signature == "" {
		writeErr(w, http.StatusNotFound, "no_signature", "version is unsigned", "")
		return
	}
	http.ServeFile(w, r, a.repo.AbsPath(v.Signature))
}