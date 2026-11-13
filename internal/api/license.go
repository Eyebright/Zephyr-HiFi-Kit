package api

import "net/http"

func (a *API) getLicense(w http.ResponseWriter, _ *http.Request) {
	l, err := a.lic.Get()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (a *API) activateLicense(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PayloadB64 string `json:"payload_b64"`
		SigB64     string `json:"sig_b64"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	l, err := a.lic.Activate(body.PayloadB64, body.SigB64)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, l)
}
