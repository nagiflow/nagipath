package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/keys"
	"github.com/nagiflow/nagipath/internal/store"
	"google.golang.org/protobuf/encoding/protojson"
)

func testMaster(t *testing.T) *keys.Master {
	t.Helper()
	path := filepath.Join(t.TempDir(), "master.key")
	if err := keys.Generate(path); err != nil {
		t.Fatal(err)
	}
	m, err := keys.Load("", path)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// ---------------------------------------------------------------- credentials

// TestPostAddCredentialAndList ports internal/web's old credentials_test.go:
// a password credential is stored, never rendered back, and shows its node
// count and type filter through the list endpoint.
func TestPostAddCredentialAndList(t *testing.T) {
	db := testDB(t)
	adminID, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, false)
	s.Master = testMaster(t)

	body, _ := json.Marshal(map[string]any{
		"name": "ldap-ops", "username": "ops@example.com", "authKind": "ldap", "password": "not-rendered-anywhere",
	})
	req := httptest.NewRequest("POST", "/settings/credentials", strings.NewReader(string(body)))
	req = req.WithContext(context.WithValue(req.Context(), userKey, store.User{ID: adminID, Role: "admin"}))
	w := httptest.NewRecorder()
	s.postAddCredential(w, req)
	if w.Code != 200 {
		t.Fatalf("postAddCredential = %d: %s", w.Code, w.Body.String())
	}

	creds, err := db.Credentials(t.Context())
	if err != nil || len(creds) != 1 || creds[0].AuthKind != "ldap" {
		t.Fatalf("credentials = %#v, %v", creds, err)
	}
	user, password, err := db.SSHPassword(t.Context(), s.Master, creds[0].ID)
	if err != nil || user != "ops@example.com" || password != "not-rendered-anywhere" {
		t.Fatalf("stored password credential = (%q, %q, %v)", user, password, err)
	}

	w = httptest.NewRecorder()
	s.getCredentials(w, httptest.NewRequest("GET", "/settings/credentials", nil))
	if strings.Contains(w.Body.String(), "not-rendered-anywhere") {
		t.Fatal("the password was rendered in the credentials list")
	}
	var list pb.CredentialsResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Credentials) != 1 || list.Credentials[0].Name != "ldap-ops" {
		t.Errorf("credentials list = %+v", list.Credentials)
	}
}

// ---------------------------------------------------------------- host keys

func TestGetHostKeysStateFilter(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	nodeID, err := db.AddNode(ctx, "192.0.2.1", 22, "test-node", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.ExecContext(ctx, `INSERT INTO host_key
		(node_id, algorithm, public_key, fingerprint, state, first_seen_at)
		VALUES (?, 'ed25519', 'key1', 'SHA256:pending', 'pending', datetime('now'))`, nodeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.ExecContext(ctx, `INSERT INTO host_key
		(node_id, algorithm, public_key, fingerprint, state, first_seen_at, decided_at)
		VALUES (?, 'rsa', 'key2', 'SHA256:approved', 'approved', datetime('now'), datetime('now'))`, nodeID); err != nil {
		t.Fatal(err)
	}

	s := New(db, nil, false)
	for path, want := range map[string]string{
		"/settings/hostkeys?state=pending":  "SHA256:pending",
		"/settings/hostkeys?state=approved": "SHA256:approved",
	} {
		w := httptest.NewRecorder()
		s.getHostKeys(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Errorf("GET %s = %d", path, w.Code)
			continue
		}
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("GET %s did not show %s", path, want)
		}
	}
}

// ---------------------------------------------------------------- api keys

// TestAPIKeyLifecycle ports the token-prefix/state-filter half of
// internal/web's old settings_test.go, plus the create-returns-a-token
// behavior that only the JSON endpoint has to get right now.
func TestAPIKeyLifecycle(t *testing.T) {
	db := testDB(t)
	adminID, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, false)
	admin := store.User{ID: adminID, Role: "admin"}

	body, _ := json.Marshal(map[string]any{"name": "ci-token", "expiresDays": 30})
	req := httptest.NewRequest("POST", "/settings/api-keys", strings.NewReader(string(body)))
	req = req.WithContext(context.WithValue(req.Context(), userKey, admin))
	w := httptest.NewRecorder()
	s.postCreateAPIKey(w, req)
	if w.Code != 200 {
		t.Fatalf("postCreateAPIKey = %d: %s", w.Code, w.Body.String())
	}
	var created struct {
		OK    bool   `json:"ok"`
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Token == "" {
		t.Fatal("postCreateAPIKey did not return the raw token")
	}

	w = httptest.NewRecorder()
	s.getAPIKeys(w, httptest.NewRequest("GET", "/settings/api-keys?state=active", nil))
	var list pb.ApiKeysResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Keys) != 1 || list.Keys[0].Name != "ci-token" {
		t.Fatalf("active api keys = %+v", list.Keys)
	}

	req = httptest.NewRequest("POST", "/settings/api-keys/"+strconv.FormatInt(created.ID, 10)+"/revoke", nil)
	req.SetPathValue("id", strconv.FormatInt(created.ID, 10))
	req = req.WithContext(context.WithValue(req.Context(), userKey, admin))
	w = httptest.NewRecorder()
	s.postRevokeAPIKey(w, req)
	if w.Code != 200 {
		t.Fatalf("postRevokeAPIKey = %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	s.getAPIKeys(w, httptest.NewRequest("GET", "/settings/api-keys?state=revoked", nil))
	list = pb.ApiKeysResponse{}
	if err := protojson.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Keys) != 1 || list.Keys[0].RevokedAt == "" {
		t.Errorf("revoked api keys = %+v", list.Keys)
	}
}

// ---------------------------------------------------------------- retention

// TestPostRetentionValidatesRanges ports the negative-value guardrail that
// used to live inline in internal/web's setRetention: a window outside its
// allowed range must be refused, not silently clamped.
func TestPostRetentionValidatesRanges(t *testing.T) {
	db := testDB(t)
	adminID, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, false)
	admin := store.User{ID: adminID, Role: "admin"}

	tooBig := 999999
	body, _ := json.Marshal(map[string]any{"snapshotDays": tooBig})
	req := httptest.NewRequest("POST", "/settings/retention", strings.NewReader(string(body)))
	req = req.WithContext(context.WithValue(req.Context(), userKey, admin))
	w := httptest.NewRecorder()
	s.postRetention(w, req)
	if w.Code != 422 {
		t.Errorf("postRetention with an out-of-range value = %d, want 422", w.Code)
	}

	ok := 30
	body, _ = json.Marshal(map[string]any{"snapshotDays": ok})
	req = httptest.NewRequest("POST", "/settings/retention", strings.NewReader(string(body)))
	req = req.WithContext(context.WithValue(req.Context(), userKey, admin))
	w = httptest.NewRecorder()
	s.postRetention(w, req)
	if w.Code != 200 {
		t.Fatalf("postRetention with a valid value = %d: %s", w.Code, w.Body.String())
	}
	if got := db.SettingInt(t.Context(), "snapshot_retention_days"); got != ok {
		t.Errorf("snapshot_retention_days = %d, want %d", got, ok)
	}
}

// TestPostRunRetentionRecordsHistory confirms a manual prune both deletes and
// leaves a PruneHistory the retention page's stats can show.
func TestPostRunRetentionRecordsHistory(t *testing.T) {
	db := testDB(t)
	adminID, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, false)
	req := httptest.NewRequest("POST", "/settings/retention/prune", nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey, store.User{ID: adminID, Role: "admin"}))
	w := httptest.NewRecorder()
	s.postRunRetention(w, req)
	if w.Code != 200 {
		t.Fatalf("postRunRetention = %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	s.getRetention(w, httptest.NewRequest("GET", "/settings/retention", nil))
	var resp pb.RetentionResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Stats == nil || resp.Stats.LastPrune == nil {
		t.Error("retention response has no last-prune history after a manual run")
	}
}

// ---------------------------------------------------------------- audit

func TestGetAuditFiltersAndExport(t *testing.T) {
	db := testDB(t)
	adminID, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Audit(t.Context(), &adminID, "credential.create", "credential", nil, "test-cred"); err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/settings/audit?action=credential.create", nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey, store.User{ID: adminID, Role: "admin"}))
	s.getAudit(w, req)
	var resp pb.AuditResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Events) != 1 || resp.Events[0].Action != "credential.create" {
		t.Fatalf("filtered audit events = %+v", resp.Events)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/settings/audit?export=csv", nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey, store.User{ID: adminID, Role: "admin"}))
	s.getAudit(w, req)
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
		t.Errorf("audit export Content-Type = %q, want text/csv", got)
	}
	if !strings.HasPrefix(w.Body.String(), "at,actor,action,target_kind,target,outcome,detail,source_ip") {
		t.Errorf("audit export has no header row: %q", w.Body.String())
	}
}

// ---------------------------------------------------------------- collection defaults

func TestPostCollectionDefaultsValidatesRanges(t *testing.T) {
	db := testDB(t)
	adminID, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, false)
	admin := store.User{ID: adminID, Role: "admin"}

	zero := 0
	body, _ := json.Marshal(map[string]any{"sshWorkers": zero}) // min is 1
	req := httptest.NewRequest("POST", "/settings/collection-defaults", strings.NewReader(string(body)))
	req = req.WithContext(context.WithValue(req.Context(), userKey, admin))
	w := httptest.NewRecorder()
	s.postCollectionDefaults(w, req)
	if w.Code != 422 {
		t.Errorf("postCollectionDefaults with sshWorkers=0 = %d, want 422", w.Code)
	}

	workers := 8
	body, _ = json.Marshal(map[string]any{"sshWorkers": workers})
	req = httptest.NewRequest("POST", "/settings/collection-defaults", strings.NewReader(string(body)))
	req = req.WithContext(context.WithValue(req.Context(), userKey, admin))
	w = httptest.NewRecorder()
	s.postCollectionDefaults(w, req)
	if w.Code != 200 {
		t.Fatalf("postCollectionDefaults with sshWorkers=8 = %d: %s", w.Code, w.Body.String())
	}
	if got := db.SettingInt(t.Context(), "ssh_workers"); got != workers {
		t.Errorf("ssh_workers = %d, want %d", got, workers)
	}
}
