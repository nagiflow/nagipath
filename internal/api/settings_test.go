package api

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/keys"
	"github.com/nagiflow/nagipath/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

// asAdmin puts an admin store.User on the context the way requireAuth would
// after a session or Bearer check — these tests call RPC methods directly,
// bypassing the HTTP auth layer entirely.
func asAdmin(ctx context.Context, adminID int64) context.Context {
	return context.WithValue(ctx, userKey, store.User{ID: adminID, Role: "admin"})
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
	ss := &settingsService{s: s}
	ctx := asAdmin(t.Context(), adminID)

	if _, err := ss.AddCredential(ctx, &pb.AddCredentialRequest{
		Name: "ldap-ops", Username: "ops@example.com", AuthKind: "ldap", Password: "not-rendered-anywhere",
	}); err != nil {
		t.Fatalf("AddCredential: %v", err)
	}

	creds, err := db.Credentials(t.Context())
	if err != nil || len(creds) != 1 || creds[0].AuthKind != "ldap" {
		t.Fatalf("credentials = %#v, %v", creds, err)
	}
	user, password, err := db.SSHPassword(t.Context(), s.Master, creds[0].ID)
	if err != nil || user != "ops@example.com" || password != "not-rendered-anywhere" {
		t.Fatalf("stored password credential = (%q, %q, %v)", user, password, err)
	}

	list, err := ss.GetCredentials(ctx, &pb.GetCredentialsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Credentials) != 1 || list.Credentials[0].Name != "ldap-ops" {
		t.Errorf("credentials list = %+v", list.Credentials)
	}
	if body, _ := protoJSON.Marshal(list); strings.Contains(string(body), "not-rendered-anywhere") {
		t.Fatal("the password was rendered in the credentials list")
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

	adminID, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, false)
	ss := &settingsService{s: s}
	rctx := asAdmin(ctx, adminID)

	for state, want := range map[string]string{"pending": "SHA256:pending", "approved": "SHA256:approved"} {
		resp, err := ss.GetHostKeys(rctx, &pb.GetHostKeysRequest{State: state})
		if err != nil {
			t.Errorf("GetHostKeys state=%s: %v", state, err)
			continue
		}
		found := false
		for _, k := range resp.Keys {
			if k.Key.Fingerprint == want {
				found = true
			}
		}
		if !found {
			t.Errorf("GetHostKeys state=%s did not include %s", state, want)
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
	ss := &settingsService{s: s}
	ctx := asAdmin(t.Context(), adminID)

	created, err := ss.CreateApiKey(ctx, &pb.CreateApiKeyRequest{Name: "ci-token", ExpiresDays: 30})
	if err != nil {
		t.Fatalf("CreateApiKey: %v", err)
	}
	if created.Token == "" {
		t.Fatal("CreateApiKey did not return the raw token")
	}

	list, err := ss.GetApiKeys(ctx, &pb.GetApiKeysRequest{State: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Keys) != 1 || list.Keys[0].Name != "ci-token" {
		t.Fatalf("active api keys = %+v", list.Keys)
	}

	if _, err := ss.RevokeApiKey(ctx, &pb.ApiKeyIdRequest{Id: created.Id}); err != nil {
		t.Fatalf("RevokeApiKey: %v", err)
	}

	list, err = ss.GetApiKeys(ctx, &pb.GetApiKeysRequest{State: "revoked"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Keys) != 1 || list.Keys[0].RevokedAt == "" {
		t.Errorf("revoked api keys = %+v", list.Keys)
	}
}

// ---------------------------------------------------------------- retention

// TestSetRetentionValidatesRanges ports the negative-value guardrail that
// used to live inline in internal/web's setRetention: a window outside its
// allowed range must be refused, not silently clamped.
func TestSetRetentionValidatesRanges(t *testing.T) {
	db := testDB(t)
	adminID, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, false)
	ss := &settingsService{s: s}
	ctx := asAdmin(t.Context(), adminID)

	tooBig := int32(999999)
	if _, err := ss.SetRetention(ctx, &pb.SetRetentionRequest{SnapshotDays: &tooBig}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("SetRetention with an out-of-range value = %v, want InvalidArgument", err)
	}

	ok := int32(30)
	if _, err := ss.SetRetention(ctx, &pb.SetRetentionRequest{SnapshotDays: &ok}); err != nil {
		t.Fatalf("SetRetention with a valid value: %v", err)
	}
	if got := db.SettingInt(t.Context(), "snapshot_retention_days"); got != int(ok) {
		t.Errorf("snapshot_retention_days = %d, want %d", got, ok)
	}
}

// TestRunRetentionRecordsHistory confirms a manual prune both deletes and
// leaves a PruneHistory the retention page's stats can show.
func TestRunRetentionRecordsHistory(t *testing.T) {
	db := testDB(t)
	adminID, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, false)
	ss := &settingsService{s: s}
	ctx := asAdmin(t.Context(), adminID)

	if _, err := ss.RunRetention(ctx, &pb.Empty{}); err != nil {
		t.Fatalf("RunRetention: %v", err)
	}

	resp, err := ss.GetRetention(ctx, &pb.Empty{})
	if err != nil {
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
	ss := &settingsService{s: s}
	ctx := asAdmin(t.Context(), adminID)

	resp, err := ss.GetAudit(ctx, &pb.GetAuditRequest{Action: "credential.create"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Events) != 1 || resp.Events[0].Action != "credential.create" {
		t.Fatalf("filtered audit events = %+v", resp.Events)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/settings/audit?export=csv", nil).WithContext(ctx)
	s.getAuditCSV(w, req)
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
		t.Errorf("audit export Content-Type = %q, want text/csv", got)
	}
	if !strings.HasPrefix(w.Body.String(), "at,actor,action,target_kind,target,outcome,detail,source_ip") {
		t.Errorf("audit export has no header row: %q", w.Body.String())
	}
}

// ---------------------------------------------------------------- collection defaults

func TestSetCollectionDefaultsValidatesRanges(t *testing.T) {
	db := testDB(t)
	adminID, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, false)
	ss := &settingsService{s: s}
	ctx := asAdmin(t.Context(), adminID)

	zero := int32(0) // min is 1
	if _, err := ss.SetCollectionDefaults(ctx, &pb.SetCollectionDefaultsRequest{SshWorkers: &zero}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("SetCollectionDefaults with sshWorkers=0 = %v, want InvalidArgument", err)
	}

	workers := int32(8)
	if _, err := ss.SetCollectionDefaults(ctx, &pb.SetCollectionDefaultsRequest{SshWorkers: &workers}); err != nil {
		t.Fatalf("SetCollectionDefaults with sshWorkers=8: %v", err)
	}
	if got := db.SettingInt(t.Context(), "ssh_workers"); got != int(workers) {
		t.Errorf("ssh_workers = %d, want %d", got, workers)
	}
}
