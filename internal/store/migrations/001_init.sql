-- 001_init — the whole v1 schema. Forward-only; never edit after release.
-- Authoritative prose: docs/backend/schema.md

CREATE TABLE setting (
  key         TEXT PRIMARY KEY,
  value       TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  updated_by  INTEGER
) STRICT;

CREATE TABLE app_user (
  id            INTEGER PRIMARY KEY,
  username      TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role          TEXT NOT NULL CHECK (role IN ('admin','viewer')),
  display_name  TEXT NOT NULL DEFAULT '',
  auth_source   TEXT NOT NULL DEFAULT 'local' CHECK (auth_source IN ('local')),
  must_change_password INTEGER NOT NULL DEFAULT 0 CHECK (must_change_password IN (0,1)),
  created_at    TEXT NOT NULL,
  last_login_at TEXT,
  disabled_at   TEXT
) STRICT;

CREATE TABLE user_session (
  token_hash  TEXT PRIMARY KEY,
  user_id     INTEGER NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
  created_at  TEXT NOT NULL,
  expires_at  TEXT NOT NULL,
  user_agent  TEXT NOT NULL DEFAULT '',
  remote_addr TEXT NOT NULL DEFAULT ''
) STRICT;

CREATE TABLE api_token (
  id           INTEGER PRIMARY KEY,
  name         TEXT NOT NULL,
  token_hash   TEXT NOT NULL UNIQUE,
  user_id      INTEGER NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
  created_at   TEXT NOT NULL,
  last_used_at TEXT,
  expires_at   TEXT,
  revoked_at   TEXT
) STRICT;

CREATE TABLE audit_event (
  id            INTEGER PRIMARY KEY,
  at            TEXT NOT NULL,
  actor_user_id INTEGER REFERENCES app_user(id) ON DELETE SET NULL,
  actor_label   TEXT NOT NULL,
  action        TEXT NOT NULL,
  target_kind   TEXT NOT NULL DEFAULT '',
  target_id     INTEGER,
  target_label  TEXT NOT NULL DEFAULT '',
  outcome       TEXT NOT NULL CHECK (outcome IN ('success','failure','denied')),
  detail        TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(detail)),
  remote_addr   TEXT NOT NULL DEFAULT ''
) STRICT;

CREATE INDEX audit_event_at ON audit_event(at DESC);
CREATE INDEX audit_event_target ON audit_event(target_kind, target_id, at DESC);

CREATE TABLE credential (
  id                 INTEGER PRIMARY KEY,
  name               TEXT NOT NULL UNIQUE,
  username           TEXT NOT NULL,
  auth_kind          TEXT NOT NULL CHECK (auth_kind IN ('private_key','ssh_certificate')),
  private_key_ct     BLOB NOT NULL,
  private_key_nonce  BLOB NOT NULL,
  passphrase_ct      BLOB,
  passphrase_nonce   BLOB,
  certificate_ct     BLOB,
  certificate_nonce  BLOB,
  public_key         TEXT NOT NULL DEFAULT '',
  key_fingerprint    TEXT NOT NULL DEFAULT '',
  created_at         TEXT NOT NULL,
  created_by         INTEGER REFERENCES app_user(id),
  rotated_at         TEXT
) STRICT;

CREATE TABLE cluster (
  id                      INTEGER PRIMARY KEY,
  name                    TEXT NOT NULL UNIQUE,
  description             TEXT NOT NULL DEFAULT '',
  golden_peer_instance_id INTEGER,
  credential_id           INTEGER REFERENCES credential(id) ON DELETE SET NULL,
  created_at              TEXT NOT NULL
) STRICT;

CREATE TABLE node (
  id                   INTEGER PRIMARY KEY,
  address              TEXT NOT NULL,
  ssh_port             INTEGER NOT NULL DEFAULT 22,
  display_name         TEXT NOT NULL,
  ssh_username         TEXT,
  credential_id        INTEGER REFERENCES credential(id) ON DELETE SET NULL,
  bastion_node_id      INTEGER REFERENCES node(id) ON DELETE SET NULL,
  os_family            TEXT NOT NULL DEFAULT 'unknown' CHECK (os_family IN ('linux','unknown')),
  sudo_available       INTEGER NOT NULL DEFAULT 0 CHECK (sudo_available IN (0,1)),
  enabled              INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  source               TEXT NOT NULL CHECK (source IN ('manual','ansible_inventory')),
  source_detail        TEXT NOT NULL DEFAULT '',
  notes                TEXT NOT NULL DEFAULT '',
  consecutive_failures INTEGER NOT NULL DEFAULT 0,
  last_collection_id   INTEGER,
  first_seen_at        TEXT NOT NULL,
  retired_at           TEXT,
  UNIQUE (address, ssh_port)
) STRICT;

CREATE TABLE host_key (
  id            INTEGER PRIMARY KEY,
  node_id       INTEGER NOT NULL REFERENCES node(id) ON DELETE CASCADE,
  algorithm     TEXT NOT NULL,
  public_key    TEXT NOT NULL,
  fingerprint   TEXT NOT NULL,
  state         TEXT NOT NULL CHECK (state IN ('pending','approved','rejected','superseded')),
  first_seen_at TEXT NOT NULL,
  decided_at    TEXT,
  decided_by    INTEGER REFERENCES app_user(id),
  superseded_at TEXT,
  UNIQUE (node_id, algorithm, fingerprint)
) STRICT;

CREATE TABLE instance (
  id               INTEGER PRIMARY KEY,
  node_id          INTEGER NOT NULL REFERENCES node(id) ON DELETE CASCADE,
  cluster_id       INTEGER REFERENCES cluster(id) ON DELETE SET NULL,
  vendor           TEXT NOT NULL CHECK (vendor IN ('nginx','apache','haproxy')),
  natural_key      TEXT NOT NULL,
  display_name     TEXT NOT NULL,
  version          TEXT NOT NULL DEFAULT '',
  binary_path      TEXT NOT NULL DEFAULT '',
  config_root      TEXT NOT NULL DEFAULT '',
  main_config_path TEXT NOT NULL DEFAULT '',
  build_flags      TEXT NOT NULL DEFAULT '',
  service_manager  TEXT NOT NULL DEFAULT 'unknown'
                     CHECK (service_manager IN ('systemd','sysvinit','container','unknown')),
  unit_name        TEXT NOT NULL DEFAULT '',
  detected_pid     INTEGER,
  access_log_paths TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(access_log_paths)),
  first_seen_at    TEXT NOT NULL,
  last_seen_at     TEXT NOT NULL,
  retired_at       TEXT,
  UNIQUE (node_id, vendor, natural_key)
) STRICT;

CREATE TABLE collection (
  id             INTEGER PRIMARY KEY,
  node_id        INTEGER NOT NULL REFERENCES node(id) ON DELETE CASCADE,
  trigger        TEXT NOT NULL CHECK (trigger IN ('scheduled','manual','first_contact')),
  actor_user_id  INTEGER REFERENCES app_user(id),
  started_at     TEXT NOT NULL,
  finished_at    TEXT,
  status         TEXT NOT NULL CHECK (status IN ('running','succeeded','degraded','failed')),
  error          TEXT NOT NULL DEFAULT '',
  instances_seen INTEGER NOT NULL DEFAULT 0,
  bytes_stored   INTEGER NOT NULL DEFAULT 0,
  duration_ms    INTEGER
) STRICT;

CREATE INDEX collection_node_started ON collection(node_id, started_at DESC);

CREATE TABLE blob (
  sha256     TEXT PRIMARY KEY,
  bytes_raw  INTEGER NOT NULL,
  bytes_zstd INTEGER NOT NULL,
  content    BLOB NOT NULL,
  created_at TEXT NOT NULL
) STRICT;

CREATE TABLE snapshot (
  id              INTEGER PRIMARY KEY,
  collection_id   INTEGER NOT NULL REFERENCES collection(id) ON DELETE CASCADE,
  instance_id     INTEGER NOT NULL REFERENCES instance(id) ON DELETE CASCADE,
  captured_at     TEXT NOT NULL,
  config_source   TEXT NOT NULL CHECK (config_source IN ('vendor_dump','fallback_walk')),
  degraded        INTEGER NOT NULL CHECK (degraded IN (0,1)),
  degraded_reason TEXT NOT NULL DEFAULT '',
  content_sha256  TEXT NOT NULL,
  file_count      INTEGER NOT NULL,
  bytes_raw       INTEGER NOT NULL,
  parse_state     TEXT NOT NULL CHECK (parse_state IN ('pending','parsed','failed')),
  parse_error     TEXT NOT NULL DEFAULT '',
  parser_version  INTEGER,
  is_current      INTEGER NOT NULL DEFAULT 0 CHECK (is_current IN (0,1)),
  UNIQUE (instance_id, captured_at)
) STRICT;

CREATE INDEX snapshot_instance_captured ON snapshot(instance_id, captured_at DESC);
CREATE UNIQUE INDEX snapshot_one_current ON snapshot(instance_id) WHERE is_current = 1;

CREATE TABLE snapshot_file (
  id          INTEGER PRIMARY KEY,
  snapshot_id INTEGER NOT NULL REFERENCES snapshot(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL CHECK (kind IN ('config_file','vendor_dump','log_format_sample')),
  path        TEXT NOT NULL,
  blob_sha256 TEXT NOT NULL REFERENCES blob(sha256),
  bytes_raw   INTEGER NOT NULL,
  truncated   INTEGER NOT NULL DEFAULT 0 CHECK (truncated IN (0,1)),
  UNIQUE (snapshot_id, kind, path)
) STRICT;

-- Derived topology. Every table below carries the derived-row contract (schema.md §1.1).

CREATE TABLE listener (
  id              INTEGER PRIMARY KEY,
  snapshot_id     INTEGER NOT NULL REFERENCES snapshot(id) ON DELETE CASCADE,
  instance_id     INTEGER NOT NULL REFERENCES instance(id),
  parser_version  INTEGER NOT NULL,
  natural_key     TEXT NOT NULL,
  ordinal         INTEGER NOT NULL,
  prov_file_id    INTEGER NOT NULL REFERENCES snapshot_file(id) ON DELETE CASCADE,
  prov_byte_start INTEGER NOT NULL,
  prov_byte_end   INTEGER NOT NULL,
  address    TEXT NOT NULL,
  port       INTEGER NOT NULL,
  tls        INTEGER NOT NULL CHECK (tls IN (0,1)),
  protocol   TEXT NOT NULL CHECK (protocol IN ('http','https','http2','http3','tcp','unknown')),
  is_default INTEGER NOT NULL CHECK (is_default IN (0,1)),
  raw_text   TEXT NOT NULL,
  UNIQUE (instance_id, snapshot_id, natural_key, ordinal)
) STRICT;

CREATE TABLE site (
  id              INTEGER PRIMARY KEY,
  snapshot_id     INTEGER NOT NULL REFERENCES snapshot(id) ON DELETE CASCADE,
  instance_id     INTEGER NOT NULL REFERENCES instance(id),
  parser_version  INTEGER NOT NULL,
  natural_key     TEXT NOT NULL,
  ordinal         INTEGER NOT NULL,
  prov_file_id    INTEGER NOT NULL REFERENCES snapshot_file(id) ON DELETE CASCADE,
  prov_byte_start INTEGER NOT NULL,
  prov_byte_end   INTEGER NOT NULL,
  listener_ids  TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(listener_ids)),
  primary_name  TEXT NOT NULL DEFAULT '',
  kind          TEXT NOT NULL CHECK (kind IN ('nginx_server','apache_vhost','haproxy_frontend')),
  document_root TEXT NOT NULL DEFAULT '',
  raw_text      TEXT NOT NULL,
  UNIQUE (instance_id, snapshot_id, natural_key, ordinal)
) STRICT;

CREATE TABLE site_name (
  id         INTEGER PRIMARY KEY,
  site_id    INTEGER NOT NULL REFERENCES site(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  match_kind TEXT NOT NULL CHECK (match_kind IN ('exact','wildcard_prefix','wildcard_suffix','regex','catch_all')),
  ordinal    INTEGER NOT NULL,
  UNIQUE (site_id, name)
) STRICT;

CREATE INDEX site_name_lookup ON site_name(name);

CREATE TABLE upstream (
  id              INTEGER PRIMARY KEY,
  snapshot_id     INTEGER NOT NULL REFERENCES snapshot(id) ON DELETE CASCADE,
  instance_id     INTEGER NOT NULL REFERENCES instance(id),
  parser_version  INTEGER NOT NULL,
  natural_key     TEXT NOT NULL,
  ordinal         INTEGER NOT NULL,
  prov_file_id    INTEGER NOT NULL REFERENCES snapshot_file(id) ON DELETE CASCADE,
  prov_byte_start INTEGER NOT NULL,
  prov_byte_end   INTEGER NOT NULL,
  name           TEXT NOT NULL,
  kind           TEXT NOT NULL CHECK (kind IN ('nginx_upstream','nginx_inline','apache_balancer',
                                               'apache_inline','haproxy_backend')),
  balance_method TEXT NOT NULL DEFAULT '',
  raw_text       TEXT NOT NULL,
  UNIQUE (instance_id, snapshot_id, natural_key, ordinal)
) STRICT;

CREATE TABLE upstream_member (
  id              INTEGER PRIMARY KEY,
  snapshot_id     INTEGER NOT NULL REFERENCES snapshot(id) ON DELETE CASCADE,
  instance_id     INTEGER NOT NULL REFERENCES instance(id),
  parser_version  INTEGER NOT NULL,
  natural_key     TEXT NOT NULL,
  ordinal         INTEGER NOT NULL,
  prov_file_id    INTEGER NOT NULL REFERENCES snapshot_file(id) ON DELETE CASCADE,
  prov_byte_start INTEGER NOT NULL,
  prov_byte_end   INTEGER NOT NULL,
  upstream_id INTEGER NOT NULL REFERENCES upstream(id) ON DELETE CASCADE,
  host        TEXT NOT NULL,
  port        INTEGER,
  scheme      TEXT NOT NULL DEFAULT '' CHECK (scheme IN ('','http','https','fcgi','uwsgi','ajp','tcp')),
  weight      INTEGER,
  flags       TEXT NOT NULL DEFAULT '',
  raw_text    TEXT NOT NULL,
  UNIQUE (instance_id, snapshot_id, natural_key, ordinal)
) STRICT;

CREATE INDEX upstream_member_host ON upstream_member(host);

CREATE TABLE route (
  id              INTEGER PRIMARY KEY,
  snapshot_id     INTEGER NOT NULL REFERENCES snapshot(id) ON DELETE CASCADE,
  instance_id     INTEGER NOT NULL REFERENCES instance(id),
  parser_version  INTEGER NOT NULL,
  natural_key     TEXT NOT NULL,
  ordinal         INTEGER NOT NULL,
  prov_file_id    INTEGER NOT NULL REFERENCES snapshot_file(id) ON DELETE CASCADE,
  prov_byte_start INTEGER NOT NULL,
  prov_byte_end   INTEGER NOT NULL,
  site_id         INTEGER NOT NULL REFERENCES site(id) ON DELETE CASCADE,
  parent_route_id INTEGER REFERENCES route(id) ON DELETE CASCADE,
  match_type      TEXT NOT NULL CHECK (match_type IN (
                    'exact','prefix','prefix_no_regex','regex','regex_ci','named',
                    'directory','directory_match','location','location_match',
                    'files','files_match','haproxy_acl_use_backend','haproxy_default_backend')),
  pattern         TEXT NOT NULL,
  precedence_rank INTEGER NOT NULL,
  specificity     INTEGER NOT NULL,
  upstream_id     INTEGER REFERENCES upstream(id) ON DELETE SET NULL,
  target_raw      TEXT NOT NULL DEFAULT '',
  is_terminal     INTEGER NOT NULL CHECK (is_terminal IN (0,1)),
  raw_text        TEXT NOT NULL,
  UNIQUE (instance_id, snapshot_id, natural_key, ordinal)
) STRICT;

CREATE INDEX route_site_precedence ON route(site_id, precedence_rank, specificity DESC, ordinal);

CREATE TABLE rule (
  id              INTEGER PRIMARY KEY,
  snapshot_id     INTEGER NOT NULL REFERENCES snapshot(id) ON DELETE CASCADE,
  instance_id     INTEGER NOT NULL REFERENCES instance(id),
  parser_version  INTEGER NOT NULL,
  natural_key     TEXT NOT NULL,
  ordinal         INTEGER NOT NULL,
  prov_file_id    INTEGER NOT NULL REFERENCES snapshot_file(id) ON DELETE CASCADE,
  prov_byte_start INTEGER NOT NULL,
  prov_byte_end   INTEGER NOT NULL,
  scope_kind   TEXT NOT NULL CHECK (scope_kind IN ('global','http','site','route','upstream','listener')),
  scope_id     INTEGER,
  directive    TEXT NOT NULL,
  action_class TEXT NOT NULL CHECK (action_class IN (
                 'match','rewrite','redirect','header','auth','cache',
                 'rate_limit','proxy','access_control','other')),
  args         TEXT NOT NULL DEFAULT '',
  raw_text     TEXT NOT NULL,
  is_modelled  INTEGER NOT NULL CHECK (is_modelled IN (0,1)),
  -- A Rule that is configured but cannot take effect, because a nearer scope
  -- discards the whole inherited set (NGINX add_header). hop_rule.shadowed is the
  -- per-request answer; this is the inventory-wide one.
  shadowed     INTEGER NOT NULL DEFAULT 0 CHECK (shadowed IN (0,1)),
  shadowed_by  TEXT NOT NULL DEFAULT '',
  UNIQUE (instance_id, snapshot_id, natural_key, ordinal)
) STRICT;

CREATE INDEX rule_scope ON rule(scope_kind, scope_id, ordinal);
CREATE INDEX rule_class ON rule(instance_id, action_class);
CREATE INDEX rule_directive ON rule(directive);

CREATE VIRTUAL TABLE rule_fts USING fts5(
  raw_text, directive,
  content='rule', content_rowid='id',
  tokenize="unicode61 separators '/.:-_'"
);

CREATE VIRTUAL TABLE snapshot_text_fts USING fts5(
  path, body,
  tokenize="unicode61 separators '/.:-_'"
);

CREATE TABLE snapshot_text_fts_map (
  rowid            INTEGER PRIMARY KEY,
  snapshot_file_id INTEGER NOT NULL REFERENCES snapshot_file(id) ON DELETE CASCADE
) STRICT;

CREATE TABLE dns_resolution (
  id          INTEGER PRIMARY KEY,
  node_id     INTEGER NOT NULL REFERENCES node(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  addresses   TEXT NOT NULL CHECK (json_valid(addresses)),
  method      TEXT NOT NULL CHECK (method IN ('getent_hosts','host','unresolved')),
  resolved_at TEXT NOT NULL,
  UNIQUE (node_id, name, resolved_at)
) STRICT;

CREATE INDEX dns_resolution_name ON dns_resolution(name, resolved_at DESC);

CREATE TABLE application (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,
  owner       TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL,
  created_by  INTEGER REFERENCES app_user(id)
) STRICT;

CREATE TABLE entry_point (
  id             INTEGER PRIMARY KEY,
  application_id INTEGER NOT NULL REFERENCES application(id) ON DELETE CASCADE,
  scheme         TEXT NOT NULL CHECK (scheme IN ('http','https')),
  hostname       TEXT NOT NULL,
  path_prefix    TEXT NOT NULL DEFAULT '/',
  port           INTEGER,
  notes          TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL,
  UNIQUE (application_id, scheme, hostname, path_prefix)
) STRICT;

CREATE TABLE trace (
  id              INTEGER PRIMARY KEY,
  entry_point_id  INTEGER REFERENCES entry_point(id) ON DELETE CASCADE,
  ad_hoc_scheme   TEXT,
  ad_hoc_hostname TEXT,
  ad_hoc_path     TEXT,
  ad_hoc_port     INTEGER,
  computed_at     TEXT NOT NULL,
  parser_version  INTEGER NOT NULL,
  snapshot_set    TEXT NOT NULL CHECK (json_valid(snapshot_set)),
  hop_count       INTEGER NOT NULL,
  confidence      TEXT NOT NULL CHECK (confidence IN ('inferred','observed_effect','verified','partial')),
  terminal_reason TEXT NOT NULL CHECK (terminal_reason IN (
                    'external_hop','no_matching_listener','no_matching_site','no_matching_route',
                    'unresolvable_upstream','static_content','hop_limit','loop_detected')),
  CHECK ((entry_point_id IS NOT NULL) <> (ad_hoc_hostname IS NOT NULL))
) STRICT;

CREATE TABLE hop (
  id              INTEGER PRIMARY KEY,
  trace_id        INTEGER NOT NULL REFERENCES trace(id) ON DELETE CASCADE,
  ordinal         INTEGER NOT NULL,
  is_external     INTEGER NOT NULL CHECK (is_external IN (0,1)),
  instance_id     INTEGER REFERENCES instance(id) ON DELETE SET NULL,
  snapshot_id     INTEGER REFERENCES snapshot(id) ON DELETE SET NULL,
  listener_id     INTEGER REFERENCES listener(id) ON DELETE SET NULL,
  site_id         INTEGER REFERENCES site(id) ON DELETE SET NULL,
  route_id        INTEGER REFERENCES route(id) ON DELETE SET NULL,
  upstream_id     INTEGER REFERENCES upstream(id) ON DELETE SET NULL,
  external_target TEXT NOT NULL DEFAULT '',
  external_reason TEXT NOT NULL DEFAULT '',
  inbound_path    TEXT NOT NULL DEFAULT '',
  effective_path  TEXT NOT NULL,
  confidence      TEXT NOT NULL CHECK (confidence IN ('inferred','observed_effect','verified')),
  UNIQUE (trace_id, ordinal)
) STRICT;

CREATE TABLE hop_rule (
  id                      INTEGER PRIMARY KEY,
  hop_id                  INTEGER NOT NULL REFERENCES hop(id) ON DELETE CASCADE,
  rule_id                 INTEGER NOT NULL REFERENCES rule(id) ON DELETE CASCADE,
  ordinal                 INTEGER NOT NULL,
  inherited_from_route_id INTEGER REFERENCES route(id) ON DELETE SET NULL,
  shadowed                INTEGER NOT NULL DEFAULT 0 CHECK (shadowed IN (0,1)),
  confidence              TEXT NOT NULL CHECK (confidence IN ('candidate','observed_effect','verified')),
  UNIQUE (hop_id, rule_id)
) STRICT;

CREATE TABLE probe (
  id                INTEGER PRIMARY KEY,
  trace_id          INTEGER REFERENCES trace(id) ON DELETE SET NULL,
  entry_point_id    INTEGER REFERENCES entry_point(id) ON DELETE SET NULL,
  actor_user_id     INTEGER NOT NULL REFERENCES app_user(id),
  method            TEXT NOT NULL CHECK (method IN ('GET','HEAD')),
  url               TEXT NOT NULL,
  correlation_token TEXT NOT NULL UNIQUE,
  origin_host       TEXT NOT NULL,
  requested_at      TEXT NOT NULL,
  completed_at      TEXT,
  status_code       INTEGER,
  redirect_chain    TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(redirect_chain)),
  response_headers  TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(response_headers)),
  duration_ms       INTEGER,
  result            TEXT NOT NULL CHECK (result IN ('running','completed','failed','blocked')),
  error             TEXT NOT NULL DEFAULT ''
) STRICT;

CREATE TABLE probe_evidence (
  id            INTEGER PRIMARY KEY,
  probe_id      INTEGER NOT NULL REFERENCES probe(id) ON DELETE CASCADE,
  hop_id        INTEGER REFERENCES hop(id) ON DELETE SET NULL,
  rule_id       INTEGER REFERENCES rule(id) ON DELETE SET NULL,
  instance_id   INTEGER REFERENCES instance(id) ON DELETE SET NULL,
  kind          TEXT NOT NULL CHECK (kind IN ('response_header','response_status','redirect',
                                              'access_log_line','auth_challenge','header_absent')),
  log_path      TEXT NOT NULL DEFAULT '',
  raw_evidence  TEXT NOT NULL,
  parsed_fields TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(parsed_fields)),
  grants        TEXT NOT NULL CHECK (grants IN ('observed_effect','verified','disproved')),
  observed_at   TEXT NOT NULL
) STRICT;

CREATE TABLE certificate (
  id                  INTEGER PRIMARY KEY,
  fingerprint_sha256  TEXT NOT NULL UNIQUE,
  subject_cn          TEXT NOT NULL DEFAULT '',
  subject_dn          TEXT NOT NULL DEFAULT '',
  sans                TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(sans)),
  issuer_dn           TEXT NOT NULL DEFAULT '',
  serial              TEXT NOT NULL DEFAULT '',
  not_before          TEXT NOT NULL,
  not_after           TEXT NOT NULL,
  key_algorithm       TEXT NOT NULL DEFAULT '',
  key_bits            INTEGER,
  signature_algorithm TEXT NOT NULL DEFAULT '',
  is_self_signed      INTEGER NOT NULL DEFAULT 0 CHECK (is_self_signed IN (0,1)),
  is_ca               INTEGER NOT NULL DEFAULT 0 CHECK (is_ca IN (0,1)),
  first_seen_at       TEXT NOT NULL,
  last_seen_at        TEXT NOT NULL
) STRICT;

CREATE INDEX certificate_expiry ON certificate(not_after);

CREATE TABLE certificate_binding (
  id              INTEGER PRIMARY KEY,
  certificate_id  INTEGER NOT NULL REFERENCES certificate(id) ON DELETE CASCADE,
  instance_id     INTEGER NOT NULL REFERENCES instance(id) ON DELETE CASCADE,
  snapshot_id     INTEGER NOT NULL REFERENCES snapshot(id) ON DELETE CASCADE,
  listener_id     INTEGER REFERENCES listener(id) ON DELETE SET NULL,
  site_id         INTEGER REFERENCES site(id) ON DELETE SET NULL,
  file_path       TEXT NOT NULL,
  combined_pem    INTEGER NOT NULL DEFAULT 0 CHECK (combined_pem IN (0,1)),
  chain_depth     INTEGER NOT NULL DEFAULT 0,
  prov_file_id    INTEGER REFERENCES snapshot_file(id) ON DELETE SET NULL,
  prov_byte_start INTEGER,
  prov_byte_end   INTEGER,
  observed_at     TEXT NOT NULL,
  UNIQUE (snapshot_id, certificate_id, file_path)
) STRICT;

CREATE TABLE drift_ignore_rule (
  id          INTEGER PRIMARY KEY,
  cluster_id  INTEGER NOT NULL REFERENCES cluster(id) ON DELETE CASCADE,
  object_kind TEXT NOT NULL,
  field       TEXT NOT NULL DEFAULT '',
  pattern     TEXT NOT NULL,
  reason      TEXT NOT NULL,
  created_at  TEXT NOT NULL,
  created_by  INTEGER REFERENCES app_user(id)
) STRICT;

CREATE TABLE drift_run (
  id                   INTEGER PRIMARY KEY,
  instance_id          INTEGER NOT NULL REFERENCES instance(id) ON DELETE CASCADE,
  subject_snapshot_id  INTEGER NOT NULL REFERENCES snapshot(id) ON DELETE CASCADE,
  baseline_kind        TEXT NOT NULL CHECK (baseline_kind IN
                         ('previous_snapshot','golden_peer','cluster_majority')),
  baseline_snapshot_id INTEGER REFERENCES snapshot(id) ON DELETE CASCADE,
  computed_at          TEXT NOT NULL,
  parser_version       INTEGER NOT NULL,
  finding_count        INTEGER NOT NULL DEFAULT 0,
  ignored_count        INTEGER NOT NULL DEFAULT 0,
  UNIQUE (instance_id, subject_snapshot_id, baseline_kind)
) STRICT;

CREATE TABLE drift_finding (
  id                 INTEGER PRIMARY KEY,
  drift_run_id       INTEGER NOT NULL REFERENCES drift_run(id) ON DELETE CASCADE,
  object_kind        TEXT NOT NULL CHECK (object_kind IN
                       ('listener','site','site_name','route','upstream','upstream_member','rule','certificate_binding')),
  natural_key        TEXT NOT NULL,
  change             TEXT NOT NULL CHECK (change IN ('added','removed','changed','reordered')),
  field              TEXT NOT NULL DEFAULT '',
  baseline_text      TEXT NOT NULL DEFAULT '',
  subject_text       TEXT NOT NULL DEFAULT '',
  action_class       TEXT NOT NULL DEFAULT '',
  prov_file_id       INTEGER REFERENCES snapshot_file(id) ON DELETE SET NULL,
  prov_byte_start    INTEGER,
  prov_byte_end      INTEGER,
  ignored_by_rule_id INTEGER REFERENCES drift_ignore_rule(id) ON DELETE SET NULL
) STRICT;

CREATE INDEX drift_finding_run ON drift_finding(drift_run_id, ignored_by_rule_id);

CREATE TABLE license_state (
  id                INTEGER PRIMARY KEY CHECK (id = 1),
  license_blob      TEXT NOT NULL,
  customer          TEXT NOT NULL,
  edition           TEXT NOT NULL,
  node_ceiling      INTEGER NOT NULL,
  expires_at        TEXT NOT NULL,
  signature_valid   INTEGER NOT NULL CHECK (signature_valid IN (0,1)),
  last_evaluated_at TEXT NOT NULL,
  installed_by      INTEGER REFERENCES app_user(id)
) STRICT;

CREATE TABLE job (
  id                   INTEGER PRIMARY KEY,
  kind                 TEXT NOT NULL CHECK (kind IN
                         ('collect_node','prune_snapshots','recompute_drift','reparse_current',
                          'gc_blobs','refresh_certificates')),
  target_kind          TEXT NOT NULL DEFAULT '',
  target_id            INTEGER,
  interval_seconds     INTEGER NOT NULL,
  jitter_seconds       INTEGER NOT NULL DEFAULT 0,
  next_run_at          TEXT NOT NULL,
  leased_until         TEXT,
  last_run_at          TEXT,
  last_status          TEXT NOT NULL DEFAULT 'never'
                         CHECK (last_status IN ('never','succeeded','degraded','failed','skipped')),
  last_error           TEXT NOT NULL DEFAULT '',
  consecutive_failures INTEGER NOT NULL DEFAULT 0,
  enabled              INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  UNIQUE (kind, target_kind, target_id)
) STRICT;

CREATE INDEX job_due ON job(next_run_at) WHERE enabled = 1;
