package api

import (
	"errors"
	"net/http"

	"hifihub/server/internal/catalog"
	"hifihub/server/internal/zatserver"
)

// nodeDevices lists the registered playback outputs merged with the live
// report, so the console can render activation and naming controls.
func (a *API) nodeDevices(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rows, err := a.nodes.Devices(id)
	if err != nil {
		writeServiceError(w, http.StatusNotFound, err)
		return
	}
	var st zatserver.PlayStatus
	if a.zat != nil {
		st, _ = a.zat.Status(id)
	}
	names := map[string]string{}
	for _, d := range st.DeviceInfo {
		names[d.HW] = d.Name
	}
	present := map[string]bool{}
	for _, hw := range st.Devices {
		present[hw] = true
	}
	out := make([]map[string]any, 0, len(rows))
	for _, d := range rows {
		reported := d.ReportedName
		if reported == "" {
			reported = names[d.HW]
		}
		out = append(out, map[string]any{
			"hw": d.HW, "name": d.Name, "reported_name": reported,
			"enabled": d.Enabled, "sort": d.Sort, "volume": d.Volume, "device_key": id + "::" + d.HW,
			"present": d.HW == "default" || present[d.HW], "online": st.Connected,
			"dsp_enabled": d.DSPEnabled, "dsd_mode": d.DSDMode,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": out})
}

func (a *API) setNodeDeviceDSDMode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
	}
	if err := decode(r, &body); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON")
		return
	}
	if err := a.nodes.SetDeviceDSDMode(r.PathValue("id"), r.PathValue("hw"), body.Mode); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dsd_mode": body.Mode})
}

func (a *API) nodeDeviceActivate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &body); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON")
		return
	}
	if err := a.nodes.SetDeviceEnabled(r.PathValue("id"), r.PathValue("hw"), body.Enabled); err != nil {
		writeServiceError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "device_key": r.PathValue("id") + "::" + r.PathValue("hw")})
}

func (a *API) nodeDeviceRename(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decode(r, &body); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON")
		return
	}
	if err := a.nodes.RenameDevice(r.PathValue("id"), r.PathValue("hw"), body.Name); err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeServiceError(w, http.StatusNotFound, err)
			return
		}
		writeErrorCode(w, http.StatusBadRequest, "device_name_invalid", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "device_key": r.PathValue("id") + "::" + r.PathValue("hw")})
}
