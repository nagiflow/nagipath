package store

import (
	"path/filepath"
	"testing"

	"github.com/nagiflow/nagipath/internal/parse"
)

func instanceCountsTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "instance_counts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// Instances() populates RouteCount and CertCount from the current snapshot's
// derived data, so an instance with parsed routes shows that count rather than
// requiring a second query at render time.
func TestInstanceCountsFromDerivedData(t *testing.T) {
	db := instanceCountsTestDB(t)
	ctx := t.Context()

	nodeID, err := db.AddNode(ctx, "10.90.4.2", 22, "web01", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}

	instID, err := db.UpsertInstance(ctx, Instance{
		NodeID: nodeID, Vendor: "nginx", NaturalKey: "/etc/nginx/nginx.conf",
		DisplayName: "web01 nginx", Version: "nginx/1.24.0",
		MainConfigPath: "/etc/nginx/nginx.conf",
	})
	if err != nil {
		t.Fatal(err)
	}

	colID, _ := db.StartCollection(ctx, nodeID, "manual", nil)

	// A configuration with routes so RouteCount has something to count.
	config := `events {} http { server { listen 80; location / { root /var/www; } location /api { proxy_pass http://backend; } } }`
	files := []parse.File{{Path: "/etc/nginx/nginx.conf", Content: []byte(config)}}

	snapID, err := db.WriteSnapshot(ctx, Snapshot{
		InstanceID: instID, CollectionID: colID, CapturedAt: Now(), ConfigSource: "vendor_dump",
	}, []SnapshotFile{{Path: files[0].Path, Kind: "vendor_dump", Content: files[0].Content}})
	if err != nil {
		t.Fatal(err)
	}

	if err := db.SaveDerived(ctx, snapID, instID, parse.NGINX(files, files[0].Path)); err != nil {
		t.Fatal(err)
	}

	db.FinishCollection(ctx, colID, "succeeded", "", 1, 0, 1)

	// The Instances() list view should show RouteCount from the parsed routes.
	list, err := db.Instances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d instance(s), want 1", len(list))
	}
	if list[0].RouteCount == 0 {
		t.Errorf("RouteCount = 0, want > 0 for an instance with parsed routes")
	}
	// SiteCount is also derived, so it is a canary: if it is zero, derived data
	// is not saved or not visible, and RouteCount cannot be trusted.
	if list[0].SiteCount == 0 {
		t.Errorf("SiteCount = 0; derived data was not saved or is not joined")
	}
}
