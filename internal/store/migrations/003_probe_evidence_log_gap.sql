-- Log correlation needs to say what it found even when it found nothing — a read
-- that failed (no file, no permission) or one that succeeded with no matching
-- line — not only when the token turned up. Without a row for those cases, a hop
-- that stayed Inferred for a log reason was indistinguishable on screen from one
-- nagipath never tried to read at all.
--
-- SQLite has no ALTER on a CHECK constraint, so the table is rebuilt: new shape,
-- copy the rows across unchanged, drop the old one, rename in.
CREATE TABLE probe_evidence_new (
  id            INTEGER PRIMARY KEY,
  probe_id      INTEGER NOT NULL REFERENCES probe(id) ON DELETE CASCADE,
  hop_id        INTEGER REFERENCES hop(id) ON DELETE SET NULL,
  rule_id       INTEGER REFERENCES rule(id) ON DELETE SET NULL,
  instance_id   INTEGER REFERENCES instance(id) ON DELETE SET NULL,
  kind          TEXT NOT NULL CHECK (kind IN ('response_header','response_status','redirect',
                                              'access_log_line','auth_challenge','header_absent',
                                              'access_log_absent','access_log_error')),
  log_path      TEXT NOT NULL DEFAULT '',
  raw_evidence  TEXT NOT NULL,
  parsed_fields TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(parsed_fields)),
  grants        TEXT NOT NULL CHECK (grants IN ('observed_effect','verified','disproved','inferred')),
  observed_at   TEXT NOT NULL
) STRICT;

INSERT INTO probe_evidence_new SELECT * FROM probe_evidence;
DROP TABLE probe_evidence;
ALTER TABLE probe_evidence_new RENAME TO probe_evidence;
