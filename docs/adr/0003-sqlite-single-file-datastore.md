# SQLite is the only datastore, and Snapshot blobs live inside it

All structured data — Nodes, Credentials, Instances, Config Objects, Rules, Certificates, Drift, Traces, users, audit — lives in one SQLite file beside the binary, in WAL mode with `STRICT` tables, FTS5 for full-text search over Snapshot text, and zstd-compressed Snapshot blobs stored in the database rather than in a directory. Installation is therefore "drop the binary, run it," with no database server, no connection string, no version matrix and no DBA. In the target enterprise, requiring a Postgres install triggers a separate approval track — DBA team, backup policy, patch cadence — which this removes entirely, and backup becomes "copy one file," which is a far better air-gap story than `pg_dump` plus an object store.

The workload fits comfortably: one process, roughly ten concurrent collection workers, a handful of UI readers, and on the order of one to two million rows at 1,000 Nodes.

## Considered Options

- **SQLite only, blobs inside** (chosen)
- **PostgreSQL** — originally recommended when HA and multi-tenant SaaS were in scope; both are now out, which removed the entire argument
- **Encrypted JSON files with in-memory indexes** — rejected because every one of the four product pillars is a query (filtered fleet-wide inventory, "every Application using this Certificate fingerprint", full-text config search, Snapshot diffs, ordered Rule lookup by path). Files mean hand-writing indexes, joins, pagination, full-text search and cross-file transactions. SQLite *is* a file on the host; the real choice was one file with a query engine or many files where we write the query engine.
- **SQLite for metadata plus content-addressed blob files** — prunes and rsyncs more neatly, but forfeits the single-file backup property

## Consequences

Multi-instance HA and any future SaaS deployment require a SQL port. Every query lives in `.sql` files behind `sqlc` and avoids SQLite-specific syntax except FTS5, so that port is bounded and known work — but it is real work, not a configuration flag. Postgres becomes the trigger-driven Enterprise-tier feature if an HA control plane is ever contracted for.
