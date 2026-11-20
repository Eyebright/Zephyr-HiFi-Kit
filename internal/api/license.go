package api

import "net/http"

func (a *API) getLicense(w http.ResponseWriter, _ *http.Request) {
	l, err := a.lic.Get()
	if err != nil {
		writeServiceError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (a *API) getLicenseReceipt(w http.ResponseWriter, _ *http.Request) {
	rec, err := a.lic.Receipt()
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="license-receipt.json"`)
	writeJSON(w, http.StatusOK, rec)
}

func (a *API) activateLicense(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PayloadB64 string `json:"payload_b64"`
		SigB64     string `json:"sig_b64"`
	}
	if err := decode(r, &body); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON")
		return
	}
	l, err := a.lic.Activate(body.PayloadB64, body.SigB64)
	if err != nil {
		writeServiceError(w, http.StatusBadRequest, err, "license_activation_failed")
		return
	}
	writeJSON(w, http.StatusOK, l)
}
