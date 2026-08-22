package trace

import (
	"strings"
	"testing"
)

// Rule lookup has to be the same site and route selection the walk uses, stopped
// after one hop. These checks pin the three things that make the answer usable:
// inheritance is labelled, shadowing is reported rather than dropped, and an
// instance that does not serve the host still says so.
func TestLookupInheritanceAndShadowing(t *testing.T) {
	db := testDB(t)
	seed(t, db, "edge01", "10.90.4.2", "nginx", "/etc/nginx/nginx.conf", edge)
	top := load(t, db)

	got := Lookup(top, LookupQuery{Hostname: "shop.example.com", Path: "/api/v2", Port: 443})
	if len(got) != 1 {
		t.Fatalf("results = %d, want 1", len(got))
	}
	r := got[0]
	if r.Site == nil || r.Site.PrimaryName != "shop.example.com" {
		t.Fatalf("site = %+v, want shop.example.com", r.Site)
	}
	if r.Route == nil || r.Route.Pattern != "/api/" {
		t.Fatalf("route = %+v, want /api/", r.Route)
	}
	if r.Confidence() != "candidate" {
		t.Errorf("confidence = %q — nothing here was observed, so it cannot be more", r.Confidence())
	}

	var inherited, ordinals int
	for _, lr := range r.Rules {
		if lr.Inherited {
			inherited++
		}
		if lr.Ordinal > 0 {
			ordinals++
			if lr.Ordinal != ordinals {
				t.Errorf("ordinal %d out of order at position %d", lr.Ordinal, ordinals)
			}
		}
	}
	if inherited == 0 {
		t.Error("the server-scope add_header must appear, marked inherited")
	}

	// location / declares its own add_header, which discards every inherited one.
	// That is the finding an operator misses by reading the file.
	root := Lookup(top, LookupQuery{Hostname: "shop.example.com", Path: "/", Port: 443})[0]
	var shadowed []string
	for _, lr := range root.Rules {
		if lr.Shadowed {
			shadowed = append(shadowed, lr.Rule.Args)
		}
	}
	if len(shadowed) == 0 {
		t.Errorf("no shadowed rule reported for /, rules = %d", len(root.Rules))
	} else if !strings.Contains(strings.Join(shadowed, " "), "X-Frame-Options") {
		t.Errorf("shadowed = %v, want the inherited X-Frame-Options", shadowed)
	}
}

func TestLookupSaysWhenNothingServesTheHost(t *testing.T) {
	db := testDB(t)
	seed(t, db, "edge01", "10.90.4.2", "nginx", "/etc/nginx/nginx.conf", edge)
	top := load(t, db)

	// http on 80: nothing in the fixture listens there.
	r := Lookup(top, LookupQuery{Hostname: "shop.example.com", Path: "/", Scheme: "http"})[0]
	if len(r.Rules) != 0 || r.Reason == "" {
		t.Errorf("rules = %d, reason = %q — a port nothing listens on must be stated", len(r.Rules), r.Reason)
	}

	// A host no site claims is a different answer from an unlistened port.
	r = Lookup(top, LookupQuery{Hostname: "other.example.com", Path: "/", Port: 443})[0]
	if !strings.Contains(r.Reason, "hostname") {
		t.Errorf("reason = %q, want it to name the hostname as the miss", r.Reason)
	}
}

// An empty class filter means every class. A filter that defaults to something is
// a filter that hides things.
func TestLookupClassFilter(t *testing.T) {
	db := testDB(t)
	seed(t, db, "edge01", "10.90.4.2", "nginx", "/etc/nginx/nginx.conf", edge)
	top := load(t, db)

	all := Lookup(top, LookupQuery{Hostname: "shop.example.com", Path: "/api/v2", Port: 443})[0]
	only := Lookup(top, LookupQuery{Hostname: "shop.example.com", Path: "/api/v2", Port: 443,
		Classes: []string{"header"}})[0]
	if len(only.Rules) == 0 || len(only.Rules) >= len(all.Rules) {
		t.Fatalf("header-only = %d rules, unfiltered = %d", len(only.Rules), len(all.Rules))
	}
	for _, lr := range only.Rules {
		if lr.Rule.ActionClass != "header" {
			t.Errorf("%s is class %q, which the filter excluded", lr.Rule.Directive, lr.Rule.ActionClass)
		}
	}

	if got := Lookup(top, LookupQuery{Hostname: "shop.example.com", Path: "/api/v2", Port: 443,
		Vendors: []string{"apache"}}); len(got) != 0 {
		t.Errorf("vendor filter returned %d results for a vendor that is not in the fleet", len(got))
	}
}
