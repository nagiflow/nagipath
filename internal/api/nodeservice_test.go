package api

import (
	"context"
	"errors"
	"testing"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/collect"
	"github.com/nagiflow/nagipath/internal/sshx"
	"github.com/nagiflow/nagipath/internal/store"
)

// fakeConnHost is a no-op collect.Host: TestNodeConnection only ever calls
// Check, Facts and Close on a successful dial.
type fakeConnHost struct{ checkErr error }

func (f *fakeConnHost) Run(context.Context, sshx.Command) (sshx.Result, error) {
	return sshx.Result{}, errors.New("not used by TestNodeConnection")
}
func (f *fakeConnHost) ReadFile(context.Context, string, int64, bool) ([]byte, bool, error) {
	return nil, false, errors.New("not used by TestNodeConnection")
}
func (f *fakeConnHost) Check(context.Context) error { return f.checkErr }
func (f *fakeConnHost) Close() error                { return nil }
func (f *fakeConnHost) Facts() (string, bool)       { return "linux", false }

// fakeConnector controls what Dialer.Connect returns without opening a real
// socket — the same seam internal/collect's own tests use for fakeHost.Connect.
type fakeConnector struct {
	host    *fakeConnHost
	dialErr error
}

func (f *fakeConnector) Connect(context.Context, int64) (collect.Host, error) {
	if f.dialErr != nil {
		return nil, f.dialErr
	}
	return f.host, nil
}

func TestTestNodeConnectionReportsConnected(t *testing.T) {
	db := testDB(t)
	adminID, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	nodeID, err := db.AddNode(t.Context(), "10.90.4.9", 22, "lb09", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, false)
	s.Collector = &collect.Collector{DB: db, Dialer: &fakeConnector{host: &fakeConnHost{}}}
	ns := &nodeService{s: s}
	ctx := context.WithValue(t.Context(), userKey, store.User{ID: adminID, Role: "admin"})

	resp, err := ns.TestNodeConnection(ctx, &pb.NodeIdRequest{Id: nodeID})
	if err != nil {
		t.Fatalf("TestNodeConnection: %v", err)
	}
	if resp.Status != "connected" {
		t.Errorf("status = %q, want connected", resp.Status)
	}
	if resp.OsFamily != "linux" {
		t.Errorf("os family = %q, want linux", resp.OsFamily)
	}
}

// A pending or changed key is recorded by sshx's HostKeyCallback as a side
// effect of the failed dial itself (store.DB.CheckHostKey) — this test seeds
// that row directly to stand in for a real handshake refusing the connection.
func TestTestNodeConnectionReportsHostKeyPending(t *testing.T) {
	db := testDB(t)
	adminID, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	nodeID, err := db.AddNode(t.Context(), "10.90.4.9", 22, "lb09", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CheckHostKey(t.Context(), nodeID, "ed25519", "ssh-ed25519 AAAAtest test", "SHA256:test"); err == nil {
		t.Fatal("CheckHostKey on an unrecognized key should refuse with ErrHostKeyPending")
	}
	s := New(db, nil, false)
	s.Collector = &collect.Collector{DB: db, Dialer: &fakeConnector{dialErr: errors.New("host key awaiting approval")}}
	ns := &nodeService{s: s}
	ctx := context.WithValue(t.Context(), userKey, store.User{ID: adminID, Role: "admin"})

	resp, err := ns.TestNodeConnection(ctx, &pb.NodeIdRequest{Id: nodeID})
	if err != nil {
		t.Fatalf("TestNodeConnection: %v", err)
	}
	if resp.Status != "host_key_pending" {
		t.Fatalf("status = %q, want host_key_pending", resp.Status)
	}
	if resp.PendingKey == nil || resp.PendingKey.Fingerprint != "SHA256:test" {
		t.Errorf("pending key = %+v, want the recorded fingerprint", resp.PendingKey)
	}
	if resp.Error != "" {
		t.Errorf("error = %q, want empty once the blocking key is identified", resp.Error)
	}
}

func TestTestNodeConnectionReportsFailed(t *testing.T) {
	db := testDB(t)
	adminID, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	nodeID, err := db.AddNode(t.Context(), "10.90.4.9", 22, "lb09", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, false)
	s.Collector = &collect.Collector{DB: db, Dialer: &fakeConnector{dialErr: errors.New("dial tcp: connection refused")}}
	ns := &nodeService{s: s}
	ctx := context.WithValue(t.Context(), userKey, store.User{ID: adminID, Role: "admin"})

	resp, err := ns.TestNodeConnection(ctx, &pb.NodeIdRequest{Id: nodeID})
	if err != nil {
		t.Fatalf("TestNodeConnection: %v", err)
	}
	if resp.Status != "failed" {
		t.Fatalf("status = %q, want failed", resp.Status)
	}
	if resp.Error == "" {
		t.Error("error message missing on a failed test")
	}
}
