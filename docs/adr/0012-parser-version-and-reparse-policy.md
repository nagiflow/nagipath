# Derived objects carry a parser version; only the latest Snapshot is re-parsed on upgrade

Snapshots are immutable, but Config Objects and Rules are derived — and the parsers will change constantly in the first year. Every derived row records the parser version that produced it. On a parser upgrade, only the latest Snapshot per Instance is re-parsed; history keeps whatever parse produced it, for provenance. Re-parsing every historical Snapshot on every upgrade was rejected as an unbounded and growing backfill, and deriving on demand without storing was rejected because fleet-wide queries become unusable.

## Consequences

The non-obvious hazard is phantom Drift: a comparison spanning a parser upgrade could report a difference caused by our parser changing rather than by the customer's configuration changing. Any diff therefore re-parses **both** Snapshots at the current parser version before comparing, so phantom Drift cannot occur regardless of what version originally parsed either side.
