# Customer Deployment

**Assumes:** [architecture.md](architecture.md). **Decisions:** ADR-0001 (single binary), ADR-0003 (embedded SQLite), ADR-0014 (offline licence).
**Product:** [../product/access_and_audit.md](../product/access_and_audit.md).

---

## 1. The deployment promise

**One file. One process. One database file. No dependencies.**

```
scp nagipath user@host:/usr/local/bin/
ssh user@host 'nagipath server'
```

That is the whole installation. No container runtime, no PostgreSQL, no Redis, no message broker, no reverse proxy required, no `pip install`, no JVM.

This is not minimalism for its own sake. It is the distribution strategy. nagipath competes against "we'll write a script for that", and the deciding factor is whether an operator can get to value before their patience runs out. Every dependency is a change request, a security review, a version conflict, and a reason to postpone — and a postponed evaluation is a lost one.

It also happens to be what makes an air-gapped install trivial, and air-gapped environments are where a web-server inventory tool is most needed and least available.

---

## 2. What must be true on the host

| Requirement | Value |
|---|---|
| OS | Linux (x86-64 or arm64), or macOS/Windows for evaluation |
| Kernel/libc | None. `CGO_ENABLED=0`, statically linked. Runs on any glibc or musl. |
| Memory | 512 MB minimum, 2 GB comfortable for a few hundred Nodes |
| Disk | 1 GB for the binary, database and blobs at default retention on a 50-Node fleet |
| CPU | 2 cores. Parsing and zstd are the only CPU-bound work. |
| Network | Outbound SSH (22, or as configured) to managed Nodes. Inbound HTTP/HTTPS for the UI. **No outbound internet.** |
| Privileges | An unprivileged user. Only needs to read its own data directory and open its listen port. |

`modernc.org/sqlite` is why there is no libc requirement: a pure-Go SQLite, including FTS5, so the binary is genuinely static and genuinely cross-compiles.

---

## 3. Layout

```
/usr/local/bin/nagipath              the binary
/var/lib/nagipath/
    nagipath.db                      database  (0600)
    nagipath.db-wal                  WAL
    nagipath.db-shm                  shared memory
    master.key                       encryption key  (0600)
/etc/nagipath/
    nagipath.env                      environment file  (0600)
```

Everything is configured by flags and environment variables. **There is no configuration file format**, because a configuration file means a parser, a schema, a migration path for that schema, and documentation for all of it — and this product has nothing whose configuration is complex enough to justify that.

```
NAGIPATH_LISTEN=0.0.0.0:8080
NAGIPATH_DB=/var/lib/nagipath/nagipath.db
NAGIPATH_MASTER_KEY_FILE=/var/lib/nagipath/master.key
NAGIPATH_TLS_CERT=/etc/nagipath/tls.crt
NAGIPATH_TLS_KEY=/etc/nagipath/tls.key
NAGIPATH_LICENSE_FILE=/etc/nagipath/license.jwt
NAGIPATH_SSH_WORKERS=8
NAGIPATH_LOG_LEVEL=info
```

---

## 4. systemd

```ini
[Unit]
Description=nagipath
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=nagipath
Group=nagipath
ExecStart=/usr/local/bin/nagipath server
EnvironmentFile=/etc/nagipath/nagipath.env
Restart=on-failure
RestartSec=5s

NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/var/lib/nagipath
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
MemoryDenyWriteExecute=yes
LockPersonality=yes

[Install]
WantedBy=multi-user.target
```

`ProtectSystem=strict` with a single `ReadWritePaths` is possible because the process writes to exactly one directory. That is a security property that falls out of the architecture rather than being retrofitted onto it — and it is the kind of unit file a hardening reviewer approves without discussion.

`Restart=on-failure`, not `always`: a refusal to start because the Master Key is missing is a **correct** decision, and restarting into it forever would turn a clear failure into a log flood.

---

## 5. TLS

Three options, in the order most customers choose them:

1. **Behind their existing reverse proxy.** Most common — they already have one, and it is the fleet's own NGINX or HAProxy. Pleasingly, it also means nagipath appears in its own inventory.
2. **Direct TLS** via `NAGIPATH_TLS_CERT` / `_KEY`.
3. **Plain HTTP** on a trusted network. Permitted, with a startup log line noting that session cookies cannot be marked `Secure`.

No ACME client. It would be a network dependency in a product whose selling point is having none, and every customer with a certificate story already has a better one than we would provide.

---

## 6. Backup

Two files, and the second matters more than people expect.

**Database** — `nagipath backup --out /backup/nagipath-2026-08-21.db` uses SQLite's online backup API: consistent, no downtime, no need to stop the service. A plain `cp` of a WAL-mode database can produce a corrupt copy, so the command exists precisely so nobody does that.

**Master Key** — a separate, one-time backup to a different location. **Without it the database's stored credentials are permanently unrecoverable.**

That warning appears at generation (terminal), in Settings, and in the backup command's own output. Three places, because this is the single mistake that cannot be undone. Backing up the database alone gives an operator false confidence in a restore that will not work.

**Restore** — stop the service, replace both files, start. Nothing else is stateful; there is no cache to warm, no index to rebuild, no external system to reconcile.

---

## 7. Upgrade

```
systemctl stop nagipath
cp nagipath /usr/local/bin/nagipath
systemctl start nagipath
```

Migrations run automatically at startup, are forward-only, and are embedded in the binary. There are no down-migrations: a down-migration that loses data is worse than a forward fix, and one that does not is usually unnecessary.

**Rollback** is by restoring the pre-upgrade backup, which is why the upgrade documentation tells you to take one first. This is a deliberate trade: forward-only migrations are simpler to write and reason about, and the cost is that rollback is a restore rather than a flag.

**Parser upgrades** advance `parser_version` and enqueue re-parsing of current Snapshots. Traces and Drift runs computed at an older version are invalidated and recomputed rather than displayed — a diff computed across a parser change reports differences *we* caused, and showing it once is enough to lose the operator's trust permanently.

---

## 8. Sizing

Measured on a 4-vCPU / 8 GB VM.

| Fleet | Collection wall time | Database after 90 days | Steady-state memory |
|---|---|---|---|
| 10 Nodes | ~1 min | ~80 MB | ~120 MB |
| 50 Nodes | ~5 min | ~400 MB | ~200 MB |
| 200 Nodes | ~20 min | ~1.5 GB | ~450 MB |
| 500 Nodes | ~50 min | ~4 GB | ~900 MB |

Database growth is dominated by `snapshot_file` rows rather than blob content, because content-addressed deduplication means unchanged configuration costs one row per file per Snapshot and zero new bytes. Typical deduplication on a homogeneous fleet is 10–20×.

**The single-process ceiling is around 500 Nodes**, bounded by collection wall time rather than by memory or database size. Beyond that the answer is a second instance covering a different set of Nodes — not a cluster. `job.leased_until` exists unused in the schema so that multi-process leasing is an addition rather than a rewrite, but v1 does not ship it and does not pretend to.

---

## 9. Observability

**Logs** — structured JSON to stdout via `log/slog`, so journald or any shipper handles it. Levels via `NAGIPATH_LOG_LEVEL`. Secrets are never logged: no key material, no session or token values, no Probe tokens.

**Health** — `GET /healthz` (process alive, unauthenticated) and `GET /readyz` (database reachable, migrations applied, unauthenticated). Both suitable for a load-balancer probe.

**Metrics** — `GET /metrics` in Prometheus format, authenticated: collection successes and failures by reason, collection duration, quarantined Node count, database size, blob count, job queue depth, HTTP request duration, Probe count.

**No telemetry.** Nothing is sent anywhere. `/metrics` is scraped by the customer if they want it; nothing initiates outbound traffic on its own.

---

## 10. Security posture summary

The one-page answer for a security review. Every item is a design property, not a configuration option:

| Question | Answer |
|---|---|
| Can it change our servers? | No. Every remote command comes from a closed enum in Go source; no user-supplied command string reaches the transport. |
| What does it run? | Read-only inspection commands. Documented, with a minimal pasteable sudoers grant. |
| Does it scan our network? | Never. Nodes come only from an operator-supplied list or an imported inventory. |
| Where are our credentials? | AES-256-GCM in the database, key in a `0600` file outside it, AAD-bound to column and row. |
| Can anyone read them back? | No role, no API path, no export, no diagnostics bundle. |
| Trust on first use? | No. Host key approval is mandatory and explicit. A changed key fails. |
| Does it phone home? | No outbound connections except SSH to your Nodes and operator-initiated Probes. Licences verify offline. |
| Does it send traffic to our apps? | Only an explicitly initiated Probe: GET or HEAD, self-identifying, rate-limited, audited. |
| Is it audited? | Append-only, exempt from retention, includes denials, readable after a user is deleted. |
| Does it store private keys? | No column exists. Certificate metadata is extracted on the target host. |

---

## 11. Failure modes

| Failure | Behaviour |
|---|---|
| Master Key missing or malformed | **Refuses to start**, prints the expected path, never generates a replacement. |
| Database locked by another process | Refuses to start, naming the file. Two processes on one database file would corrupt WAL assumptions. |
| Disk full | Writes fail; reads continue; a banner appears. The UI stays usable so the operator can change retention, which is the fix. |
| Migration failure | Refuses to start with the failing migration and error. Never runs with a partially migrated schema. |
| Listen port in use | Refuses to start with the address. |
| SSH unreachable to a Node | That Node's collection fails; every other Node is unaffected. |
| Process killed mid-collection | Jobs are database rows; in-flight work is reclaimed at startup. Nothing depends on process memory. |
| Clock wrong | Schedules drift and a licence expiry may display incorrectly. Cosmetic, because enforcement is soft — and Probe correlation uses tokens rather than timestamps precisely so verification is immune to it. |
| Licence expired | Banner. Collections and Traces continue. The rule that cannot be broken. |

---

## 12. Acceptance criteria

| # | Criterion |
|---|---|
| 1 | A single static binary with no runtime dependencies, `CGO_ENABLED=0`. |
| 2 | Runs as an unprivileged user writing to one directory. |
| 3 | Configuration by flags and environment only; no configuration file format exists. |
| 4 | The provided systemd unit works with `ProtectSystem=strict`. |
| 5 | Installation to first Trace takes under 30 minutes with no internet access. |
| 6 | Migrations run automatically, are forward-only, and are embedded. |
| 7 | A failed migration prevents startup. |
| 8 | `nagipath backup` uses the online backup API and is safe while running. |
| 9 | The Master Key backup warning appears at generation, in Settings, and in the backup output. |
| 10 | The server refuses to start on a missing or malformed Master Key and never regenerates it. |
| 11 | `/healthz` and `/readyz` are unauthenticated; `/metrics` is authenticated. |
| 12 | Logs are structured JSON and contain no secrets. |
| 13 | No outbound network connection is made other than SSH to Nodes and operator-initiated Probes. |
| 14 | Parser upgrades invalidate and recompute derived data rather than displaying stale results. |
| 15 | Sizing figures are measured and published, including the ~500-Node single-process ceiling. |
