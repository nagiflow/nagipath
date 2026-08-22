# Applications

**Assumes:** [design_system.md](design_system.md). **Backend:** [application.md](../backend/application.md).
**Milestone:** M3 (with Trace).

---

## 1. What these screens are

An Application is a name, an owner, and a set of Entry Points. Everything else about it — which Instances serve it, which Certificates it depends on, which Upstreams it reaches — is **derived by tracing**, never entered by hand.

The UI has one job beyond CRUD: make that derivation obvious. An operator who thinks this is a CMDB will expect to add servers to an application and will be confused when there is nowhere to do it. So the screens are built so that the absence of a "add servers" button reads as intentional rather than missing.

Routes: `/applications`, `/applications/{id}`, `/applications/new`.

---

## 2. `/applications` — List

```
┌────────────────────────────────────────────────────────────────────────────┐
│  Applications                                        [ + New application ] │
│                                                                            │
│  ⓘ An application is a set of entry points. Its servers, upstreams and      │
│    certificates are discovered by tracing from those entry points, so the   │
│    footprint is always current.                                            │
│                                                                            │
│  payments-prod                             payments-platform@corp.example   │
│    2 entry points · 6 instances · 3 vendors · 5 nodes                       │
│    Partial   1 hop leaves the fleet                                        │
│    ⚠ certificate expires in 24d      ⚠ 2 drift findings          [ open ]  │
│                                                                            │
│  api-gateway                                    platform-eng@corp.example   │
│    1 entry point · 4 instances · 2 vendors · 3 nodes                        │
│    Verified                                                       [ open ] │
│                                                                            │
│  legacy-portal                                                     no owner │
│    1 entry point · 0 instances                                             │
│    ⚠ entry point resolves to no site in this fleet                [ open ] │
└────────────────────────────────────────────────────────────────────────────┘
```

The explanatory note stays permanently. It is the single sentence that prevents the CMDB misreading, and a dismissible note would be dismissed by the person who most needs it.

Each row leads with the footprint, because an owner opens this page to see *how many moving parts my application depends on and which of them is about to break*. Both halves of that come from tracing rather than from anything anyone typed.

Confidence badge is the **worst** across the Application's Traces. `legacy-portal` shows the honest outcome of an Entry Point that traces nowhere — a real and common state, presented as a finding rather than as zeros.

---

## 3. `/applications/new`

```
  Name          [ payments-prod                        ]
  Owner         [ payments-platform@corp.example       ]  free text in v1
  Description   [                                      ]

  Entry points
    where is this application reached?

    [ https ▾ ]  [ payments.corp.example      ]  [ /              ]  [ − ]
    [ https ▾ ]  [ api.corp.example           ]  [ /payments      ]  [ − ]
    [ + add entry point ]

  ⓘ Do not list servers. nagipath finds them by tracing from these
    hostnames, every time you look.

                                        [ Cancel ]  [ Create application ]
```

**Hostname autocompletes from `site_name` across the fleet.** The most likely user error is a hostname that is not served anywhere, and autocomplete prevents most of it while still allowing free text — because sometimes the discovery *is* that the hostname is not configured.

The note about not listing servers appears at the point of confusion. This is the form where someone will look for a server field.

**Creating returns the Application with a footprint already computed**, not a page of zeros with a "trace now" button. A newly created Application showing nothing looks broken, and the first impression of the feature is the footprint appearing immediately.

**Validation**

| Error | Message |
|---|---|
| `422` wildcard hostname | `Entry points cannot contain wildcards. An entry point is a hostname requests actually arrive at, not a pattern.` |
| `422` scheme or path in the hostname field | `Enter the hostname only. Use the scheme and path fields.` |
| `422` path not starting with `/` | `Paths must start with /` |
| `409` duplicate within this Application | Inline on the row. |
| `409` same Entry Point in another Application | **Allowed**, with a warning naming the other Application. Usually a real ownership dispute the tool should expose rather than resolve. |
| No Entry Points | Allowed. Created with an `inert` chip and a prompt to add one — a half-finished onboarding step, not an error. |

---

## 4. `/applications/{id}` — Detail

Header: name, owner, description, confidence badge, `computed_at`, actions `Retrace` · `Edit` · `Delete`.

### 4.1 Entry Points

Each row: scheme, hostname, path prefix, Hop count, confidence, terminal reason, `[ trace ]`, `[ verify ]`.

`trace` opens the Trace Explorer at that Entry Point. `verify` is admin-only and runs a Probe.

### 4.2 Footprint

```
┌──────────────────────────────────────────────────────────────────────┐
│  Footprint                                    computed 2h ago  ⟳     │
│                                                                      │
│  Instances                                                           │
│    lb01   haproxy 2.8.5   entry        hop 0                         │
│    web02  nginx 1.24.0    intermediate hop 1                         │
│    web05  nginx 1.24.0    intermediate hop 1   ⚠ 2 drift findings    │
│    app01  apache 2.4.57   terminal     hop 2                         │
│                                                                      │
│  ⚠ Leaves the fleet                                                  │
│    10.90.4.7:8443   from hop 2                                       │
│    not a managed node — the footprint is unknown beyond this point    │
│    [ add this host as a node ]                                       │
│                                                                      │
│  Certificates                                                        │
│    payments.corp.example   24d   2 bindings              ⚠           │
│    *.corp.example         118d   1 binding                           │
│                                                                      │
│  1 of 4 hops leaves the managed fleet. This footprint is complete     │
│  only up to that point.                                              │
└──────────────────────────────────────────────────────────────────────┘
```

Instances carry their **role** — entry, intermediate, terminal — derived from Hop ordinal. It is what an owner needs to reason about their own topology without learning the fleet's layout.

**The coverage note at the bottom is mandatory and never suppressed.** A footprint that silently stops at the fleet boundary reads as complete, and that is the most dangerous possible output on this screen: the operator concludes there is nothing further to check. The External Hops block and the closing sentence exist to make the boundary impossible to miss.

`add this host as a node` at an External Hop is where most fleets grow in nagipath. Turning a dead end into the next onboarding action at exactly that moment is the right move.

### 4.3 Overlaps

Shown when present:

```
  ⓘ Overlapping entry points
    api.corp.example/payments  (this application)
    api.corp.example/          api-gateway
    Your path is more specific and wins at request time.
```

Not an error. Usually a shared edge hostname, and the owner of the broader Entry Point often does not know about the narrower one. nagipath takes no position on which is correct.

### 4.4 Delete

```
  Delete payments-prod?

  This removes the application, its 2 entry points and 2 cached traces.

  Nothing about your fleet is deleted: nodes, instances, snapshots,
  certificates and probe history are untouched.

  [ Cancel ]                                     [ Delete application ]
```

The second paragraph is the point. An Application is a view over derived data, and views are cheap to discard — but the operator does not know that, and "delete application" sounds destructive. Saying what is *not* deleted removes the hesitation.

---

## 5. The reverse query

Reached from Instance detail, not from here: **every Application whose Traces cross this Instance**, with Hop ordinal and confidence.

This is the pre-decommission check — "what breaks if I take web05 out" — and it is the question a hand-maintained CMDB answers confidently and wrongly. It lives on the Instance page because that is where the person about to decommission a host is standing.

---

## 6. Loading, error and empty states

| State | Treatment |
|---|---|
| No Applications | `No applications yet.` Then: *you can trace any URL without creating one. Applications are for tracking a set of entry points over time.* The escape hatch matters — nobody should think this is a prerequisite. |
| Application with no Entry Points | `inert` chip, prompt to add one. Not an error. |
| Entry Point traces nowhere | Terminal reason in plain English plus nearby hostnames found. `no_matching_site` usually means a typo, and saying so saves a support round trip. |
| Footprint stale | Served with `⚠ recomputing…`; the stale footprint stays visible. Never a spinner over data we have. |
| Retrace running | Existing footprint dimmed with an inline spinner; the page stays usable. |
| Every contributing Snapshot unparsed | Footprint empty **with the parse failure named**, never a silent zero. |
| Trace crosses a degraded Snapshot | Confidence drops to `partial`, the degraded Instance named on its row. |
| Footprint is empty because all Traces end at hop 0 | `This application's entry points are served directly, with no proxy hops.` A complete answer, styled neutrally. |
| Owner field empty | `no owner` in muted text. Not an error; the v1 free-text limitation is real and stated in the Edit form. |
| Duplicate Entry Point across Applications | Both allowed, both flagged, other Application linked. |
| `viewer` creating an Application | **Allowed.** Deliberately — an Application is a harmless label, and making it admin-only means a developer files a ticket to ask about their own service. |
| Application deleted while a Probe runs | Probe completes; its evidence and audit rows survive. |

---

## 7. Acceptance criteria

| # | Criterion |
|---|---|
| 1 | There is no way anywhere in the UI to add a server to an Application. |
| 2 | The list page carries a permanent, non-dismissible note explaining derivation. |
| 3 | The create form states that servers are not listed, at the point of confusion. |
| 4 | Hostname input autocompletes from fleet Site names while still accepting free text. |
| 5 | Wildcards, schemes and paths in the hostname field are rejected with specific messages. |
| 6 | Creation returns a footprint already computed. |
| 7 | Footprint shows each Instance's role derived from Hop ordinal. |
| 8 | External Hops are listed with the reason and an action to add the host as a Node. |
| 9 | The coverage note is always present when any Trace leaves the fleet and is never suppressible. |
| 10 | Confidence is the worst across contributing Traces, never averaged or rounded up. |
| 11 | Overlapping Entry Points across Applications are surfaced without nagipath taking a position. |
| 12 | Delete confirmation states explicitly what fleet data is not deleted. |
| 13 | Stale footprints are served flagged rather than blocking. |
| 14 | An unparsed contributing Snapshot produces a named failure, never a silent zero. |
| 15 | The reverse query is available from Instance detail. |
| 16 | `viewer` can create Applications and Entry Points. |
| 17 | Empty state states that tracing works without an Application. |
| 18 | Every view is deep-linkable. |
