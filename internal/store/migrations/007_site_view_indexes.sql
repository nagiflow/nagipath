-- Every view query reaches derived rows by parent id or by "the rows of this
-- snapshot", and none of those columns were indexed, so each one full-scanned.
-- The Sites list was the worst case (minutes on a few thousand hostnames), but
-- the certificate list and the node Upstreams tab run correlated subqueries per
-- row and paid the same scan once per row.
CREATE INDEX site_primary_name ON site(primary_name);
CREATE INDEX site_snapshot ON site(snapshot_id);
CREATE INDEX certificate_binding_site ON certificate_binding(site_id, snapshot_id);
CREATE INDEX certificate_binding_certificate ON certificate_binding(certificate_id);
CREATE INDEX upstream_member_upstream ON upstream_member(upstream_id);
CREATE INDEX upstream_snapshot ON upstream(snapshot_id);
CREATE INDEX route_upstream ON route(upstream_id);
CREATE INDEX listener_snapshot ON listener(snapshot_id);
