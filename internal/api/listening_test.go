package api

import "testing"

func TestListeningAPI(t *testing.T) {
	base := setup(t, "dev")
	for _, query := range []string{"", "?days=1", "?days=7", "?days=30&node_id=absent"} {
		status, body := doJSON(t, "GET", base+"/listening"+query, nil)
		if status != 200 {
			t.Fatalf("%s: %d %s", query, status, body)
		}
		value := mustJSON(t, body)
		for _, key := range []string{"genres", "artists", "recent_albums"} {
			items, ok := value[key].([]any)
			if !ok || len(items) != 0 {
				t.Fatalf("%s must be [], got %v", key, value[key])
			}
		}
		if value["listen_ms"] != float64(0) || value["unmeasured_plays"] != float64(0) || value["timezone"] == "" {
			t.Fatalf("bad summary: %s", body)
		}
	}
	for _, days := range []string{"0", "2", "-1", "31", "abc"} {
		status, body := doJSON(t, "GET", base+"/listening?days="+days, nil)
		if status != 400 {
			t.Fatalf("days=%s: %d %s", days, status, body)
		}
	}
}
