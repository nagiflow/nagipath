# Test Lab and Correctness Strategy

**Decisions:** ADR-0004 (vendor tooling is the config authority), ADR-0005 (position-preserving parse), ADR-0012 (parser version and reparse policy).
**Backend:** [../backend/config_object.md](../backend/config_object.md), [../backend/rule.md](../backend/rule.md), [../backend/trace.md](../backend/trace.md), [../backend/probe.md](../backend/probe.md).

---

## 1. What this has to prove

nagipath's entire value rests on one claim: **the parse is right, and the precedence resolution is right.** A Trace that shows the wrong Hop is worse than no Trace, because the operator acts on it. An inventory that misses an Instance is worse than an empty inventory, because it is believed.

Ordinary unit tests do not establish that. `parseServerName("server_name a.com b.com;")` returning two names proves nothing about whether NGINX would have selected that `server` block. So the strategy has three layers, and the top two are what actually earn confidence:

| Layer | Proves | Cost |
|---|---|---|
| **Fixture corpus** — checked-in configs with asserted expectations | Parsing and precedence, deterministically, in CI, in under a second | Cheap, runs everywhere |
| **Container lab** — real NGINX, Apache, HAProxy serving real traffic | That our model of precedence matches what the servers actually do | Moderate, needs Docker |
| **Differential testing** — our answer vs the server's answer | Bugs nobody thought to write a test for | Moderate, the highest-yield layer |

Layer 3 is where the real bugs come from. The others confirm what we already believe.

---

## 2. The fixture corpus

```
testdata/
  nginx/
    001-simple-server/         nginx.conf  expected.json
    002-location-precedence/   nginx.conf  expected.json
    003-regex-file-order/      nginx.conf  expected.json
    004-add-header-discard/    nginx.conf  expected.json
    005-include-glob/          conf.d/*    expected.json
    ...
  apache/
    001-vhost-basic/  002-directory-vs-location/  003-rewrite-chain/  ...
  haproxy/
    001-frontend-backend/  002-acl-use-backend/  003-default-only/  ...
  multivendor/
    001-haproxy-to-nginx-to-apache/   fleet.yaml  expected-trace.json
```

Each `expected.json` asserts derived rows, not prose: Sites with their names, Listeners, Locations with `precedence_rank` and `specificity`, Upstreams and members, Rules with `action_class` and `is_modelled`, and provenance byte ranges.

### 2.1 The cases that must exist

These are the ones where an implementation is most likely to be subtly wrong, and where being wrong is invisible until a customer is misled:

| Case | What it pins |
|---|---|
| NGINX `=` / `^~` / regex / prefix in one server block | The full ordering, `= 10`, `^~ 20`, regex `30`, prefix `40` |
| Two regex locations, the *later* one more specific | **Regex order is file order.** Rank 30 ignores `specificity`. Getting this backwards is the single most likely precedence bug. |
| `^~` prefix beating a longer regex | `^~` is a **short-circuit, not a score**. A specificity-only implementation gets this wrong. |
| Nested `add_header` in server and location | The **discard** semantic: the inner one discards all inherited ones. Expected output must include a `shadowed_rules` entry. |
| `include conf.d/*.conf` with three matched files | Glob expansion, and that provenance points at the *included* file |
| `include` of a file that does not exist | Degraded parse with a named reason, not a crash |
| Apache `<Directory>` vs `<Location>` on the same path | `<Location>` wins. Reversing these is the Apache equivalent of the regex bug. |
| Apache `RewriteRule` chain with `[L]` | Chain termination and path transformation |
| Apache `<VirtualHost *:443>` with `SSLCertificateFile` | Certificate Binding extraction with provenance |
| HAProxy `use_backend` with ACLs, plus `default_backend` | Rank 90 before 99, and that the ACL condition is captured |
| HAProxy `crt` pointing at a combined PEM | `combined_pem = 1` on the Binding, and that no key material appears anywhere in the output |
| Same server block declared twice | `ordinal` disambiguation under an identical natural key |
| A directive we do not model | One Rule with `is_modelled = 0` and verbatim text, not a dropped line |
| UTF-8 and CRLF in a config file | Byte offsets stay correct. Provenance that is off by a byte is provenance nobody trusts. |
| An 8 MiB config file | The ReadFile cap behaviour, degraded rather than OOM |

### 2.2 Rules for the corpus

- **Every fixture comes from a real-world shape**, not invented syntax. Fixtures that only exist to exercise our parser test our parser against itself.
- **Every parser bug fixed adds a fixture** before the fix lands. The corpus is the regression record.
- **A `parser_version` bump requires re-running the corpus and reviewing every diff.** ADR-0012 says derived data is recomputed across versions; this is where we find out whether the recomputation changed an answer we did not intend to change.
- Fixtures are **checked in**, never generated at test time. A generated fixture that changes silently is a test that stops testing.

---

## 3. The container lab

```yaml
# testlab/docker-compose.yml   — the multivendor chain
lb01:    haproxy:2.8    :443  → web02, web05
web02:   nginx:1.24     :8080 → app01/payments      rewrites /api/v2 → /v2
web05:   nginx:1.24     :8080 → app01/payments      (deliberately diverged from web02)
app01:   httpd:2.4      :9000  terminal
extern:  (absent)              web05 also proxies to 10.90.4.7:8443, unreachable
sshd:    each container runs sshd with a fixed test key
```

This is deliberately the same topology the demo seeds and the same one the documentation uses as its worked example. One canonical fleet means a bug found in the demo is reproducible in the lab, and a documentation example that goes stale fails a test.

Each container runs `sshd` because **the SSH executor is part of what needs testing**. A mocked `Executor` proves the parser works; it proves nothing about `getent hosts` on the target, sudo failure handling, host key mismatch, an 8 MiB file, or a command timeout. Those are the failure modes customers actually hit.

The lab includes deliberate defects, because handling them correctly is a feature:

- `web05` diverges from `web02` in three ways: one meaningful (an extra `proxy_set_header`), one cosmetic (a comment), one ordering-only. Drift must report the first two and classify the third as `reordered`.
- `app01` has a config file readable only by root, to exercise the sudo path and the "not captured — permission denied" Snapshot entry.
- `extern` does not exist, so the Trace must terminate at an External Hop rather than hanging or claiming completeness.
- One certificate expires in 24 days; another has SANs that do not cover the Site serving it.

---

## 4. Differential testing — the important layer

For every fixture, ask the vendor's own tooling and compare.

```
nginx -T                    → our file set must match exactly
nginx -t                    → a config we call valid, nginx must call valid
httpd -t -D DUMP_VHOSTS     → our Sites and Listeners must match
httpd -t -D DUMP_MODULES    → our module list must match
haproxy -c -f ...           → validity agreement
```

`nginx -T` is the strongest single assertion available: it prints the complete effective configuration including every resolved `include`. **If our file set differs from `nginx -T`'s, our parse is incomplete by definition** — and an incomplete file set is how an Instance's real behaviour ends up invisible. HAProxy has no `include`, so its `-f` arguments are already the provably complete set, which is why the HAProxy adapter has the least room for this class of error.

### 4.1 Behavioural differential — precedence

The one that catches what nobody thought to test. For each of ~200 request paths against the lab:

1. Ask nagipath which Location, Rules and Upstream apply.
2. Send the real request to the real server with a distinguishing echo (a backend returning its own identity).
3. **Assert the server agrees.**

Any disagreement is a precedence bug, full stop. This test found the class of bug that the `^~`-as-a-score implementation produces, and it is the only test that would have.

### 4.2 Verification differential

Run a real Probe against the lab and assert:

- The token appears in `X-Nagipath-Probe` and reaches the access log of every Hop that handled it.
- Correlation succeeds **with clocks deliberately skewed by ten minutes** on two containers. Token-not-timestamp correlation is a design claim; this is the test that makes it one we can defend.
- A Hop whose `log_format` lacks `$upstream_addr` yields `partial` with the specific blocker named — the NGINX limitation, tested rather than assumed.
- The shadowed `add_header` produces **negative evidence**: the header is genuinely absent from the response, so a configured-but-ineffective directive is proven ineffective.

That last assertion tests the product's single most valuable output. It gets its own test.

---

## 5. Non-functional tests

| Test | Assertion |
|---|---|
| Read-only guarantee | Every remote command run in a full collection is captured and asserted against the `Command.ID` enum. **A command not in the enum fails the test.** The closed enum is the injection boundary; this is the test that keeps it closed. |
| No key material | A full collection against the lab, then grep the database and all logs for `PRIVATE KEY` and the lab's known key bytes. Zero matches. |
| Diagnostics bundle | Generate one against a fully populated database; assert no credential ciphertext, no session or token value, no Probe token, no config content. |
| No unexpected egress | Run a collection with outbound traffic captured; assert only SSH to lab hosts. No DNS to the internet, no telemetry, no licence call. |
| Scale | 500 synthetic Nodes with generated configs. Assert collection completes, memory stays bounded, and the sizing figures in [customer_deployment.md §8](customer_deployment.md) still hold. Sizing claims we publish are tested claims. |
| Concurrency | Collections running while a retention prune and a blob GC run. Assert no lock timeout, and that cursor pagination neither skips nor repeats a row while rows are being written. |
| Retention is not Trace-aware | Compute a Trace, supersede its Snapshots, prune. Assert the Snapshots are **actually deleted** — no pinning (ADR-0015) — and that the Trace now reports `reproducible: false` with reason `snapshots_pruned`, retains its Hop count, ordinals and `effective_path` values, and still exposes its `probe_evidence`. Reclaimed bytes must match what the preview predicted; a pinning regression shows up as a preview that lies. |
| Migration chain | Every released migration applied in order from empty, then the current binary against a v1.0.0-era database. |
| Crash recovery | Kill the process mid-collection; assert in-flight jobs are reclaimed and no partial Snapshot is marked current. |

---

## 6. What is not tested, and why

| Not tested | Reason |
|---|---|
| Every NGINX directive | The `is_modelled` flag exists precisely so unmodelled directives are surfaced rather than silently mishandled. `/rules/distribution` measures the backlog with real customer data, which is a better prioritisation signal than exhaustive fixtures. |
| Vendor versions outside 1.18+/2.4+/2.0+ | Stated support range. Outside it, the version banner says so. |
| Browser matrix beyond current Chrome, Firefox and Safari | Server-rendered HTML with ~400 lines of htmx glue (ADR-0013). There is not enough client-side behaviour to have a matrix. |
| Load testing the UI | Single-tenant, tens of concurrent operators. Testing for a load that will not occur is time not spent on parser correctness. |

---

## 7. CI

| Stage | Runs | Time |
|---|---|---|
| vet, staticcheck, unit tests, fixture corpus | Every push | < 60s |
| Container lab integration + differential | Every PR | ~5 min |
| Scale, migration chain, egress, no-key-material | Nightly and every release tag | ~20 min |

The fast stage is the one developers feel, so the corpus lives there — parser regressions must be caught in under a minute or the corpus stops being consulted. The differential suite is slower but gates merges, because a precedence bug reaching `main` is not an acceptable outcome.

---

## 8. Acceptance criteria

| # | Criterion |
|---|---|
| 1 | The fixture corpus runs in CI in under 60 seconds with no containers. |
| 2 | Every case in §2.1 has a fixture with asserted derived rows. |
| 3 | NGINX regex ordering is asserted as file order, not specificity. |
| 4 | `^~` short-circuit behaviour is asserted separately from specificity scoring. |
| 5 | `add_header` discard semantics are asserted, including the `shadowed_rules` output. |
| 6 | Apache `<Location>` beating `<Directory>` is asserted. |
| 7 | Byte-offset provenance is asserted correct under UTF-8 and CRLF. |
| 8 | Our file set is asserted equal to `nginx -T`'s for every NGINX fixture. |
| 9 | Sites and Listeners are asserted equal to `httpd -D DUMP_VHOSTS` output. |
| 10 | ~200 request paths are behaviourally differential-tested against the running lab; any disagreement fails. |
| 11 | Probe correlation is asserted correct with clocks skewed by ten minutes. |
| 12 | Negative evidence for a shadowed header is asserted end to end. |
| 13 | A remote command outside the `Command.ID` enum fails a test. |
| 14 | The database, logs and diagnostics bundle are asserted free of key material and secrets. |
| 15 | A collection is asserted to make no outbound connection other than SSH to Nodes. |
| 16 | Published sizing figures are produced by the nightly scale test. |
| 17 | Retention deletes Snapshots referenced by a Trace, and the affected Trace degrades to `unreproducible` while keeping its shape and its Probe evidence. |
| 18 | Every parser bug fix adds a fixture before the fix lands. |
| 19 | A `parser_version` bump requires reviewing every corpus diff. |
| 20 | The lab topology is the same one the demo seeds and the documentation uses. |
| 21 | The full migration chain is applied from empty on every release. |
