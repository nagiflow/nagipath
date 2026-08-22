# Retention never pins Snapshots for a Trace; affected Traces become unreproducible

A Trace records the Snapshots its walk used in `trace.snapshot_set`, so it can be explained months later. Retention deletes old Snapshots. When those two meet, **retention wins**: Snapshots are pruned on their own schedule and any Trace that referenced them is marked `unreproducible` and says so on screen. Nothing is pinned.

The alternative — pinning every Snapshot referenced by any Trace — was rejected. It makes storage a function of who clicked what: one operator's exploratory Trace from March holds an entire Snapshot chain alive indefinitely, retention settings stop meaning what they say, and the operator has no way to see why the database is not shrinking or to reclaim the space without deleting Traces they do not remember creating. Storage growth an operator cannot predict, explain or reclaim is a worse failure than a Trace that honestly admits it can no longer show its evidence. An earlier draft of the retention procedure hedged by pinning "Snapshots referenced by a Trace still on screen", which is not a predicate a retention query can evaluate — the hedge was the tell that pinning was wrong.

The honest alternative is cheap because the schema already implements it. Every derived-row foreign key on `hop` is `ON DELETE SET NULL` and `hop_rule.rule_id` is `ON DELETE CASCADE`, so deleting a Snapshot leaves the Trace's **shape** intact — hop count, ordinals, Instances, `effective_path` per Hop, terminal reason, the recorded confidence — while the per-Hop detail that lived in the Snapshot's derived rows disappears. That is exactly the right degradation: the answer survives, the evidence for it does not, and the difference is visible rather than inferred.

Reproducibility is **derived, not stored**. A Trace is unreproducible when any Hop has `is_external = 0 AND snapshot_id IS NULL`, or when `trace.parser_version` is behind the current one. A stored flag would be a second source of truth that can disagree with the rows it describes.

Live cached Traces are unaffected in practice: a cached Trace is invalidated as soon as any Snapshot in its `snapshot_set` loses `is_current`, and the current Snapshot per Instance is never prunable. Only Traces deliberately kept for the historical record can reach the unreproducible state — which is precisely the population this trade-off is about.

## Consequences

The retention procedure has no Trace-awareness at all, which keeps the single most dangerous operation in the schema free of a conditional nobody can test. Retention counts and reclaimable space are exactly predictable from the settings, so the pre-apply preview can state them without qualification.

The preview must warn how many Traces the change will make unreproducible, and the Trace Explorer must render the state rather than silently showing a Trace with empty Hop cards — a Trace missing its rules while looking otherwise normal is the one outcome worse than either option here. An unreproducible Trace offers `Retrace` as its primary action; recomputing against current configuration is almost always what the operator wanted, and it is what makes the degradation recoverable instead of merely honest.

Verification evidence is unaffected. `probe` and `probe_evidence` are exempt from retention, so a Trace can be unreproducible from configuration while still holding the verbatim log lines that proved it — an operator can lose the explanation and keep the proof. That asymmetry is deliberate and worth stating: evidence is small, cheap and irreplaceable, whereas configuration text is large and recollectable.
