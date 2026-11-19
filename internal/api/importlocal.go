package api

import (
	"net/http"
	"os"
	"path/filepath"

	"hifihub/server/internal/domain"
)

// importLocalVersion ingests a vendor installer already present on the server
// host (Roon Bridge tar.bz2, HQPlayer NAA deb) into the artifact repository,
// hashing and signature-checking it exactly like a browser upload.
func (a *API) importLocalVersion(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if _, err := a.cat.GetApp(appID); err != nil {
		fail(w, err)
		return
	}
	var req struct {
		domain.AppVersion
		LocalPath     string `json:"local_path"`
		SignaturePath string `json:"signature_path"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error(), "")
		return
	}
	v := req.AppVersion
	v.AppID = appID
	if v.Version == "" || v.Arch == "" {
		writeError(w, http.StatusBadRequest, "version 与 arch 不能为空")
		return
	}
	if v.Format != "deb" && v.Format != "tar.bz2" {
		writeError(w, http.StatusBadRequest, "format 仅支持 deb 或 tar.bz2")
		return
	}
	if !filepath.IsAbs(req.LocalPath) {
		writeError(w, http.StatusBadRequest, "local_path 必须是服务端本机绝对路径")
		return
	}
	f, err := os.Open(req.LocalPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无法读取本地安装包："+err.Error())
		return
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || st.IsDir() {
		writeError(w, http.StatusBadRequest, "local_path 必须指向文件")
		return
	}
	var sig []byte
	if req.SignaturePath != "" {
		sig, err = os.ReadFile(req.SignaturePath)
		if err != nil {
			writeError(w, http.StatusBadRequest, "无法读取签名文件："+err.Error())
			return
		}
	}
	res, err := a.repo.Put(appID, v.Version, v.Arch, filepath.Base(req.LocalPath), f, sig)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "artifact_rejected", err.Error(), "")
		return
	}
	v.SHA256, v.Size, v.ArtifactPath, v.Signed = res.SHA256, res.Size, res.RelPath, res.Signed
	if res.SigPath != "" {
		v.Signature = res.SigPath
	}
	v, err = a.cat.AddVersion(v)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}
