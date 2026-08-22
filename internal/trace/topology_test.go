package trace

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"sync/atomic"
	"testing"

	msqlite "modernc.org/sqlite"

	"github.com/nagiflow/nagipath/internal/store"
)

// The tests below guard against the regression this file's history warns about:
// Load and loadInstance used to run one query per Instance per kind of object
// (listener, rule, upstream, upstream member, route, site, site name) — 1 + 7N + 1
// queries for an N-Instance fleet, all of it re-run on every request that touched
// topology data, including pages that only needed one Instance. A wall-clock
// benchmark would catch that only under load and flake under everything else, so
// instead these count actual round trips to the database driver.

// countingConn wraps one real sqlite driver.Conn and counts every QueryContext
// call that reaches it — the thing both Load (a batched query per kind of object)
// and QueryRowContext (single-row lookups) go through at the driver level.
type countingConn struct {
	driver.Conn
	n *int64
}

func (c *countingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	atomic.AddInt64(c.n, 1)
	q, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		// Every codepath under test drives db.R with *Context calls, so a real
		// sqlite conn always satisfies this; ErrSkip tells database/sql to fall
		// back to Prepare+Query rather than silently miscounting.
		return nil, driver.ErrSkip
	}
	return q.QueryContext(ctx, query, args)
}

type countingConnector struct {
	dsn string
	n   *int64
}

func (c *countingConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := (&msqlite.Driver{}).Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &countingConn{Conn: conn, n: c.n}, nil
}

func (c *countingConnector) Driver() driver.Driver { return &msqlite.Driver{} }

// countedReader points db.R at a fresh connection to the same database file that
// counts every read query run through it, replacing (and closing) the pool
// store.Open built. The pragmas mirror store.go's open() exactly, since a mismatch
// there (journal mode in particular) would change how the file is read, not just
// how it is counted.
func countedReader(t *testing.T, db *store.DB) *int64 {
	t.Helper()
	n := new(int64)
	dsn := db.Path + "?_pragma=busy_timeout(10000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(ON)"
	counted := sql.OpenDB(&countingConnector{dsn: dsn, n: n})
	t.Cleanup(func() { counted.Close() })
	db.R.Close()
	db.R = counted
	return n
}

// TestLoadQueryCountDoesNotScaleWithFleetSize is the direct regression test for the
// N+1 fix: Load's query count must stay flat as the number of Instances grows,
// instead of growing by ~7 queries per additional Instance.
func TestLoadQueryCountDoesNotScaleWithFleetSize(t *testing.T) {
	ctx := t.Context()

	countFor := func(n int) int64 {
		db := testDB(t)
		for i := 0; i < n; i++ {
			node := fmt.Sprintf("web%02d", i)
			seed(t, db, node, node, "nginx", "/etc/nginx/nginx.conf", edge)
		}
		counter := countedReader(t, db)
		if _, err := Load(ctx, db); err != nil {
			t.Fatalf("Load with %d instances: %v", n, err)
		}
		return atomic.LoadInt64(counter)
	}

	const smallFleet, largeFleet = 2, 8
	small := countFor(smallFleet)
	large := countFor(largeFleet)
	t.Logf("Load query count: %d instances -> %d queries, %d instances -> %d queries",
		smallFleet, small, largeFleet, large)

	if large != small {
		t.Errorf("Load's query count scales with fleet size: %d instances = %d queries, "+
			"%d instances = %d queries; want equal (batched, not one query set per Instance)",
			smallFleet, small, largeFleet, large)
	}
	// A generous ceiling: the old code ran 1 + 7*2 + 1 = 16 queries for just the
	// 2-instance fleet, and would have run 1 + 7*8 + 1 = 58 for the 8-instance one.
	// The batched version issues a fixed 9 (1 instance list + 7 batched object
	// queries + 1 DNS query) regardless of fleet size.
	const maxQueries = 20
	if small > maxQueries {
		t.Errorf("Load ran %d queries for a %d-instance fleet, want <= %d (batched)",
			small, smallFleet, maxQueries)
	}
}

// TestLoadInstanceQueryCountIsBoundedByOneInstance is the regression test for the
// scoping fix: a page that only needs one Instance must not pay for the rest of the
// fleet. It seeds a 5-Instance fleet and asserts LoadInstance's query count for one
// of them is small and does not grow with the fleet it sits in.
func TestLoadInstanceQueryCountIsBoundedByOneInstance(t *testing.T) {
	ctx := t.Context()
	db := testDB(t)

	const fleetSize = 5
	var lastNode int64
	for i := 0; i < fleetSize; i++ {
		node := fmt.Sprintf("web%02d", i)
		lastNode = seed(t, db, node, node, "nginx", "/etc/nginx/nginx.conf", edge)
	}

	// Resolve the seeded node to its Instance ID before swapping in the counting
	// reader, so this lookup does not itself count against the assertion below.
	instances, err := db.Instances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var instID int64
	for _, in := range instances {
		if in.NodeID == lastNode {
			instID = in.ID
		}
	}
	if instID == 0 {
		t.Fatal("could not find the seeded instance")
	}

	counter := countedReader(t, db)
	inst, err := LoadInstance(ctx, db, instID)
	if err != nil {
		t.Fatal(err)
	}
	if inst == nil {
		t.Fatal("LoadInstance found nothing for a freshly seeded instance")
	}
	got := atomic.LoadInt64(counter)
	t.Logf("LoadInstance query count for 1 of %d instances: %d queries", fleetSize, got)

	// loadInstance's own shape is 7 queries (listener, rule, upstream, upstream
	// member, route, site, site name) plus the 1 row lookup LoadInstance itself
	// does = 8. A ceiling well under fleetSize*8 = 40 proves this did not fall back
	// to loading the whole fleet to answer for one Instance.
	const maxQueries = 10
	if got > maxQueries {
		t.Errorf("LoadInstance ran %d queries for one Instance in a %d-instance fleet, want <= %d",
			got, fleetSize, maxQueries)
	}
}
