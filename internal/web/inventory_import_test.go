package web

import (
	"net/url"
	"strings"
	"testing"
)

func TestInventoryImportPageIsStandalone(t *testing.T) {
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/nodes/import")
	if w.Code != 200 {
		t.Fatalf("GET /nodes/import = %d, want 200", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"Import inventory", `action="/nodes/import"`, "Select a stored credential"} {
		if !strings.Contains(body, want) {
			t.Errorf("import page missing %q", want)
		}
	}
	if strings.Contains(body, "step 1 of 4") || strings.Contains(body, "Connect fleet") {
		t.Error("import page still renders onboarding wizard content")
	}
}
