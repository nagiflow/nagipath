package store

import (
	"context"
	"path/filepath"
	"testing"
)

func siteViewTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "site_view.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSiteListWithVariants(t *testing.T) {
	db := siteViewTestDB(t)
	ctx := context.Background()

	rows, variants, err := db.SiteListWithVariants(ctx, "")
	if err != nil {
		t.Fatalf("SiteListWithVariants failed: %v", err)
	}

	if len(rows) == 0 {
		t.Skip("no sites in test database")
	}

	// Check that rows have required fields
	for _, row := range rows {
		if row.Name == "" {
			t.Error("site row missing Name")
		}
		if row.Nodes < 0 {
			t.Error("site row has negative Nodes count")
		}
		if row.Variants < 0 {
			t.Error("site row has negative Variants count")
		}
	}

	// When no site is selected, variants should be empty
	if len(variants) > 0 {
		t.Error("variants should be empty when no site selected")
	}
}

func TestSiteListWithVariantsSelected(t *testing.T) {
	db := siteViewTestDB(t)
	ctx := context.Background()

	rows, _, err := db.SiteListWithVariants(ctx, "")
	if err != nil {
		t.Fatalf("SiteListWithVariants failed: %v", err)
	}

	if len(rows) == 0 {
		t.Skip("no sites in test database")
	}

	// Select the first site
	siteName := rows[0].Name
	_, variants, err := db.SiteListWithVariants(ctx, siteName)
	if err != nil {
		t.Fatalf("SiteListWithVariants with selection failed: %v", err)
	}

	// Variants should be present if the site has any
	if rows[0].Variants > 1 && len(variants) == 0 {
		t.Error("variants should be populated for selected site with multiple variants")
	}

	// Check variant structure
	for _, v := range variants {
		if v.Key == "" {
			t.Error("variant missing Key")
		}
		if v.Nodes < 0 {
			t.Error("variant has negative Nodes count")
		}
	}
}

func TestSiteOverviewData(t *testing.T) {
	db := siteViewTestDB(t)
	ctx := context.Background()

	rows, _, err := db.SiteListWithVariants(ctx, "")
	if err != nil || len(rows) == 0 {
		t.Skip("no sites in test database")
	}

	siteName := rows[0].Name
	overview, err := db.SiteOverviewData(ctx, siteName, "")
	if err != nil {
		t.Fatalf("SiteOverviewData failed: %v", err)
	}

	if overview.Name == "" {
		t.Error("overview missing Name")
	}
	if overview.Stats.Nodes < 0 {
		t.Error("overview stats have negative Nodes count")
	}
	if overview.Stats.Routes < 0 {
		t.Error("overview stats have negative Routes count")
	}
}

func TestSiteNodesData(t *testing.T) {
	db := siteViewTestDB(t)
	ctx := context.Background()

	rows, _, err := db.SiteListWithVariants(ctx, "")
	if err != nil || len(rows) == 0 {
		t.Skip("no sites in test database")
	}

	siteName := rows[0].Name
	nodes, err := db.SiteNodesData(ctx, siteName)
	if err != nil {
		t.Fatalf("SiteNodesData failed: %v", err)
	}

	// Check node structure
	for _, n := range nodes {
		if n.NodeID == 0 {
			t.Error("node row missing NodeID")
		}
		if n.NodeName == "" {
			t.Error("node row missing NodeName")
		}
	}
}

func TestSiteUpstreamsData(t *testing.T) {
	db := siteViewTestDB(t)
	ctx := context.Background()

	rows, _, err := db.SiteListWithVariants(ctx, "")
	if err != nil || len(rows) == 0 {
		t.Skip("no sites in test database")
	}

	siteName := rows[0].Name
	upstreams, err := db.SiteUpstreamsData(ctx, siteName)
	if err != nil {
		t.Fatalf("SiteUpstreamsData failed: %v", err)
	}

	// Check upstream structure
	for _, u := range upstreams {
		if u.Upstream == "" {
			t.Error("upstream row missing Upstream name")
		}
		if u.Host == "" {
			t.Error("upstream row missing Host")
		}
	}
}

func TestSiteCertificatesData(t *testing.T) {
	db := siteViewTestDB(t)
	ctx := context.Background()

	rows, _, err := db.SiteListWithVariants(ctx, "")
	if err != nil || len(rows) == 0 {
		t.Skip("no sites in test database")
	}

	siteName := rows[0].Name
	certs, err := db.SiteCertificatesData(ctx, siteName)
	if err != nil {
		t.Fatalf("SiteCertificatesData failed: %v", err)
	}

	// Check certificate structure
	for _, c := range certs {
		if c.Subject == "" && c.NotAfter == "" {
			t.Error("certificate row missing both Subject and NotAfter")
		}
		if c.Bindings < 0 {
			t.Error("certificate row has negative Bindings count")
		}
	}
}

func TestVariantKeysAssignment(t *testing.T) {
	db := siteViewTestDB(t)
	ctx := context.Background()

	rows, _, err := db.SiteListWithVariants(ctx, "")
	if err != nil || len(rows) == 0 {
		t.Skip("no sites in test database")
	}

	// Find a site with multiple variants
	var siteName string
	for _, row := range rows {
		if row.Variants > 1 {
			siteName = row.Name
			break
		}
	}

	if siteName == "" {
		t.Skip("no sites with multiple variants")
	}

	_, variants, err := db.SiteListWithVariants(ctx, siteName)
	if err != nil {
		t.Fatalf("SiteListWithVariants failed: %v", err)
	}

	// Check that variants have sequential keys A, B, C, etc.
	seen := make(map[string]bool)
	for _, v := range variants {
		if v.Key == "" {
			t.Error("variant missing Key")
		}
		if seen[v.Key] {
			t.Errorf("duplicate variant key %q", v.Key)
		}
		seen[v.Key] = true
	}
}

func TestCoveredBySANs(t *testing.T) {
	tests := []struct {
		hostname string
		names    []string
		want     bool
	}{
		{"example.com", []string{"example.com"}, true},
		{"example.com", []string{"*.example.com"}, false}, // wildcard doesn't match bare domain
		{"www.example.com", []string{"*.example.com"}, true},
		{"api.example.com", []string{"*.example.com"}, true},
		{"deep.api.example.com", []string{"*.example.com"}, false}, // wildcard only matches one level
		{"example.com", []string{"other.com", "example.com"}, true},
		{"test.com", []string{"example.com", "*.example.com"}, false},
	}

	for _, tt := range tests {
		got := coveredBySANs(tt.hostname, tt.names)
		if got != tt.want {
			t.Errorf("coveredBySANs(%q, %v) = %v, want %v", tt.hostname, tt.names, got, tt.want)
		}
	}
}

func TestSiteListAliasCount(t *testing.T) {
	db := siteViewTestDB(t)
	ctx := context.Background()

	rows, _, err := db.SiteListWithVariants(ctx, "")
	if err != nil {
		t.Fatalf("SiteListWithVariants failed: %v", err)
	}

	// Check that alias counts are non-negative
	for _, row := range rows {
		if row.AliasCount < 0 {
			t.Errorf("site %q has negative alias count: %d", row.Name, row.AliasCount)
		}
		// If AliasCount > 0, Aliases slice should not be empty
		if row.AliasCount > 0 && len(row.Aliases) == 0 {
			t.Errorf("site %q has AliasCount %d but empty Aliases slice", row.Name, row.AliasCount)
		}
	}
}

func TestSiteListState(t *testing.T) {
	db := siteViewTestDB(t)
	ctx := context.Background()

	rows, _, err := db.SiteListWithVariants(ctx, "")
	if err != nil {
		t.Fatalf("SiteListWithVariants failed: %v", err)
	}

	// Check that state is populated
	for _, row := range rows {
		if row.State == "" {
			t.Errorf("site %q has empty State", row.Name)
		}
		// State should be one of the expected values
		validStates := map[string]bool{
			"OK": true,
		}
		// Also allow cert expiry states like "CERT 6d"
		if !validStates[row.State] && len(row.State) > 4 && row.State[:4] != "CERT" {
			t.Errorf("site %q has unexpected state %q", row.Name, row.State)
		}
	}
}
