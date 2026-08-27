-- Credentials started as SSH-key-only rows. Rebuild the table because SQLite
-- cannot alter its CHECK constraint. Existing encrypted key material is copied
-- byte-for-byte.
-- Node and cluster rows may point at a credential while this replacement is
-- in flight. Defer those references to the migration transaction's commit,
-- where the renamed replacement table is again named `credential`.
PRAGMA defer_foreign_keys = ON;

CREATE TABLE credential_new (
  id                 INTEGER PRIMARY KEY,
  name               TEXT NOT NULL UNIQUE,
  username           TEXT NOT NULL,
  auth_kind          TEXT NOT NULL CHECK (auth_kind IN ('private_key', 'ssh_certificate', 'username_password', 'kerberos', 'ldap', 'cyberark')),
  private_key_ct     BLOB NOT NULL,
  private_key_nonce  BLOB NOT NULL,
  passphrase_ct      BLOB,
  passphrase_nonce   BLOB,
  certificate_ct     BLOB,
  certificate_nonce  BLOB,
  password_ct        BLOB,
  password_nonce     BLOB,
  external_ref       TEXT NOT NULL DEFAULT '',
  public_key         TEXT NOT NULL DEFAULT '',
  key_fingerprint    TEXT NOT NULL DEFAULT '',
  created_at         TEXT NOT NULL,
  created_by         INTEGER REFERENCES app_user(id),
  rotated_at         TEXT
) STRICT;

INSERT INTO credential_new (id, name, username, auth_kind, private_key_ct, private_key_nonce, passphrase_ct, passphrase_nonce, certificate_ct, certificate_nonce, public_key, key_fingerprint, created_at, created_by, rotated_at)
SELECT id, name, username, auth_kind, private_key_ct, private_key_nonce, passphrase_ct, passphrase_nonce, certificate_ct, certificate_nonce, public_key, key_fingerprint, created_at, created_by, rotated_at
FROM credential;

DROP TABLE credential;
ALTER TABLE credential_new RENAME TO credential;
