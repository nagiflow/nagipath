package web

import (
	"net/url"
	"strings"
	"testing"
)

const testPrivateKey = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACBKjOmntBSro1ndD+eIwP42a+NMIblJQSqvNpY9tPR4AgAAAJi1g/r/tYP6
/wAAAAtzc2gtZWQyNTUxOQAAACBKjOmntBSro1ndD+eIwP42a+NMIblJQSqvNpY9tPR4Ag
AAAEB2ROXtat6XEeEl08vk8V8C4iFKTFkxxurJfZOucR9ZTkqM6ae0FKujWd0P54jA/jZr
40whuUlBKq82lj209HgCAAAAEW5hZ2lwYXRoIHRlc3Qga2V5AQIDBA==
-----END OPENSSH PRIVATE KEY-----
`

func TestCredentialsTypeFilter(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	if _, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	// The Type filter select should exist and render in the template.
	w := c.get("/settings/credentials?type=private_key")
	body := w.Body.String()
	if !strings.Contains(body, "Type:") {
		t.Error("Type filter select not found in template")
	}
}

func TestCredentialsCreateUsernamePassword(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	if _, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})
	w := c.post("/settings/credentials", url.Values{
		"name": {"ldap-ops"}, "username": {"ops@example.com"},
		"auth_kind": {"ldap"}, "password": {"not-rendered-anywhere"},
	})
	if w.Code != 303 {
		t.Fatalf("POST password credential = %d: %s", w.Code, w.Body.String())
	}
	creds, err := db.Credentials(ctx)
	if err != nil || len(creds) != 1 {
		t.Fatalf("credentials = %#v, %v", creds, err)
	}
	if creds[0].AuthKind != "ldap" {
		t.Fatalf("kind = %q, want ldap", creds[0].AuthKind)
	}
	user, password, err := db.SSHPassword(ctx, s.Master, creds[0].ID)
	if err != nil || user != "ops@example.com" || password != "not-rendered-anywhere" {
		t.Fatalf("stored password credential = (%q, %q, %v)", user, password, err)
	}
	page := c.get("/settings/credentials").Body.String()
	if strings.Contains(page, "not-rendered-anywhere") {
		t.Fatal("password was rendered")
	}
}

func TestCredentialsNodeCount(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	if _, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}

	// Create a credential and a node using it.
	credID, err := db.CreateCredential(ctx, s.Master, "fleet-key", "nagipath", "private_key",
		testPrivateKey, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddNode(ctx, "10.90.4.20", 22, "test-node-01", "nagipath", &credID, nil, "manual", nil); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/settings/credentials")
	body := w.Body.String()
	// The Nodes column header should be present.
	if !strings.Contains(body, "Nodes") {
		t.Error("Nodes column header not found")
	}
	// The row should show the count next to the credential.
	if !strings.Contains(body, "fleet-key") || !strings.Contains(body, ">1<") {
		t.Error("Node count of 1 not displayed beside credential")
	}
}

func TestCredentialsLastUsed(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	if _, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}

	// Create a credential, node, and collection to ensure last_used is populated.
	credID, err := db.CreateCredential(ctx, s.Master, "used-key", "ops", "private_key",
		testPrivateKey, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	nodeID, err := db.AddNode(ctx, "10.90.4.21", 22, "collected-node", "nagipath", &credID, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	colID, err := db.StartCollection(ctx, nodeID, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishCollection(ctx, colID, "succeeded", "", 0, 0, 100); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/settings/credentials")
	body := w.Body.String()
	// The Last used column header should be present.
	if !strings.Contains(body, "Last used") {
		t.Error("Last used column header not found")
	}
	// A credential with a collection should show a timestamp, not em-dash.
	if strings.Contains(body, "used-key") {
		// Look for the time representation - should NOT be just em-dash.
		lines := strings.Split(body, "\n")
		var foundUsedKey bool
		for i, line := range lines {
			if strings.Contains(line, "used-key") && i+1 < len(lines) {
				// Check subsequent lines for the last-used cell.
				nextLines := strings.Join(lines[i:min(i+5, len(lines))], "\n")
				if strings.Contains(nextLines, "Last used") && strings.Contains(nextLines, "—") && !strings.Contains(nextLines, "Z") {
					t.Error("Last used showing only em-dash for a credential that was used")
				}
				foundUsedKey = true
				break
			}
		}
		if !foundUsedKey {
			t.Error("Credential 'used-key' not found in output")
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
