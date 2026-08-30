package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReadAPIRequiresAndAcceptsAPIKey(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	userID, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := db.CreateAPIToken(ctx, "ci", userID, 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		token string
		want  int
	}{
		{"", http.StatusUnauthorized},
		{"wrong", http.StatusUnauthorized},
		{token, http.StatusOK},
	} {
		r := httptest.NewRequest("GET", "/api/nodes", nil)
		if tc.token != "" {
			r.Header.Set("Authorization", "Bearer "+tc.token)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("token %q: status %d, want %d: %s", tc.token, w.Code, tc.want, w.Body.String())
		}
	}
}
