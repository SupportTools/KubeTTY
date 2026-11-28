package admin

import (
	"encoding/json"
	"net/http"
	"testing"
)

// cluster-services-1376: imageAutoFollow is the per-project opt-out. Saving
// it is a setting only and must never roll the pod.

func TestUpdateProject_ImageAutoFollowSavesWithoutRollout(t *testing.T) {
	for _, val := range []bool{false, true} {
		store := newMockProjectStore()
		p := runningProject()
		p.ImageAutoFollow = !val
		store.projects[p.ID] = p
		h := NewProjectHandlers(store, nil, "1.16.1")

		rec := putProject(t, h, p.ID, map[string]any{"imageAutoFollow": val})
		if rec.Code != http.StatusOK {
			t.Fatalf("imageAutoFollow=%v: status = %d, body %s", val, rec.Code, rec.Body.String())
		}
		if p.ImageAutoFollow != val {
			t.Errorf("imageAutoFollow=%v: stored %v", val, p.ImageAutoFollow)
		}
		if store.calledStatus != "" {
			t.Errorf("imageAutoFollow=%v: SetStatus(%q) called; the opt-out must not roll the pod", val, store.calledStatus)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body["imageAutoFollow"] != val {
			t.Errorf("response imageAutoFollow = %v, want %v", body["imageAutoFollow"], val)
		}
	}
}
