package store

import (
	"path/filepath"
	"testing"

	"github.com/nagiflow/nagipath/internal/parse"
)

// NodeStats feeds the five tiles on the node detail overview, and its caller
// discards the error — so a broken column name reads as "this node serves
// nothing" rather than as a failure. This asserts the query actually runs.
func TestNodeStatsCountsDerivedData(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "node_view.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := t.Context()

	nodeID, err := db.AddNode(ctx, "10.90.4.2", 22, "web01", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	instID, err := db.UpsertInstance(ctx, Instance{
		NodeID: nodeID, Vendor: "nginx", NaturalKey: "/etc/nginx/nginx.conf",
		DisplayName: "web01 nginx", MainConfigPath: "/etc/nginx/nginx.conf",
	})
	if err != nil {
		t.Fatal(err)
	}
	colID, _ := db.StartCollection(ctx, nodeID, "manual", nil)

	config := `events {} http { upstream backend { server 10.90.4.9:8080; }
		server { listen 80; location / { root /var/www; } location /api { proxy_pass http://backend; } } }`
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

	s, err := db.NodeStats(ctx, instID)
	if err != nil {
		t.Fatalf("NodeStats: %v", err)
	}
	if s.Sites == 0 {
		t.Error("Sites = 0, want the parsed server block")
	}
	if s.Routes == 0 {
		t.Error("Routes = 0, want the parsed locations")
	}
	if s.Upstreams == 0 {
		t.Error("Upstreams = 0, want the parsed upstream block")
	}
}
