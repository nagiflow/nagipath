# Trace Explorer

**Assumes:** [design_system.md](design_system.md). **Backend:** [trace.md](../backend/trace.md), [probe.md](../backend/probe.md).
**Product:** [../product/request_path_trace.md](../product/request_path_trace.md), [../product/trace_verification.md](../product/trace_verification.md).
**Milestone:** M3 (trace), M8 (verify).

---

## 1. What this screen is

The flagship. It is the screen in the demo, the screen open at 3am, and the screen a prospect judges the product by. Everything else in the UI can be competent; this one has to be good.

It answers one question — *where does this request go* — and it answers it with a chain of Hops, each labelled with how much nagipath actually knows.

Route: `/trace`. Deep link: `/trace?url=https://payments.corp.example/api/v2/charge`, which is shareable and is the form pasted into tickets.

---

## 2. Layout

```
┌────────────────────────────────────────────────────────────────────────────┐
│  Trace                                                                     │
│  ┌──────────────────────────────────────────────────────┐  ┌────────────┐  │
│  │ https://payments.corp.example/api/v2/charge          │  │   Trace    │  │
│  └──────────────────────────────────────────────────────┘  └────────────┘  │
│  or pick an entry point ▾                                                  │
├────────────────────────────────────────────────────────────────────────────┤
│  Partial   4 hops   ends: leaves the fleet   collected 2h ago   [ Verify ] │
│  ⟳ Recompute    ⧉ Copy link    ↓ Export                    ⊞ Graph ☰ Table │
├────────────────────────────────────────────────────────────────────────────┤
│                                                                            │
│   ┌──────────────────────────────────────────────────────────────────┐     │
│   │ 1  lb01.corp.example · haproxy 2.8.5                  Verified   │     │
│   │    ────────────────────────────────────────────────────────────  │     │
│   │    listener   0.0.0.0:443  TLS                                   │     │
│   │    site       fe_https        acl hdr_beg(host) payments         │     │
│   │    route      path_beg /api   use_backend, file order, first     │     │
│   │               match wins                        haproxy.cfg:64   │     │
│   │    upstream   be_payments  roundrobin                            │     │
│   │    path       /api/v2/charge  →  /api/v2/charge   unchanged       │     │
│   │    rules      1 header                                    show ▾ │     │
│   └──────────────────────────────────────────────────────────────────┘     │
│                    │ be_payments → web02.corp.example:443                  │
│                    │ 10.20.1.12  resolved on lb01           Verified       │
│                    ▼                                                       │
│   ┌──────────────────────────────────────────────────────────────────┐     │
│   │ 2  web02.corp.example · nginx 1.24.0                  Verified   │     │
│   │    ────────────────────────────────────────────────────────────  │     │
│   │    site       payments.corp.example   exact server_name          │     │
│   │    route      prefix /api   longest matching prefix; no = or ^~  │     │
│   │               matched, and no regex matched first  api.conf:14   │     │
│   │    path       /api/v2/charge  →  /v2/charge                      │     │
│   │               rewrite ^/api/(.*) /$1 break        api.conf:18    │     │
│   │  ⚠ 1 rule configured here is not in effect                show ▾ │     │
│   │    rules      6 in effect                                 show ▾ │     │
│   └──────────────────────────────────────────────────────────────────┘     │
│                    ├── app01.corp.example:8080  → hop 3                    │
│                    └── app02.corp.example:8080  backup  → hop 4  Inferred  │
│   … hop 3 …                                                                │
│   ┌──────────────────────────────────────────────────────────────────┐     │
│   │ 4  10.90.3.11:8080                       Leaves the fleet        │     │
│   │    app02.corp.example resolves to an address that is not a       │     │
│   │    managed node. nagipath cannot see beyond this point.          │     │
│   │    [ Add this host as a node ]                                   │     │
│   └──────────────────────────────────────────────────────────────────┘     │
│                                                                            │
│  ⚠ 1 branch could not be evaluated                                 show ▾ │
└────────────────────────────────────────────────────────────────────────────┘
```

Table view is the default. The graph is a toggle.

That is deliberate and worth defending: the graph demos better, but the table reads better, prints better, pastes into a ticket better, works with a screen reader, and is what someone actually uses at 3am. Defaulting to the graph would be optimising the screen for the demo over the job.

---

## 3. The header strip

Six elements, all load-bearing:

| Element | Behaviour |
|---|---|
| **Confidence badge** | Worst of all Hops. Hover explains why it is not higher. |
| **Hop count** | |
| **Terminal reason** | Plain English, always present. Never "unknown". |
| **Freshness** | `collected 2h ago`, absolute on hover. If any Snapshot is stale: `⚠ recomputing…` and the stale result is shown meanwhile rather than a spinner. |
| **Verify** | Admin only. Disabled for `viewer` with the reason on hover; disabled in demo mode with "probes are disabled in demo mode". |
| **Copy link / Export** | Copies the deep link; exports Markdown or JSON. Markdown because the destination is a ticket or an incident channel. |

---

## 4. The Hop card

### 4.1 Always visible

Ordinal, Node, Instance with Vendor and version, confidence badge, and then the resolution chain: Listener → Site (with how it matched) → Route (with **why it won**) → Upstream.

Then the path row, which is the most-looked-at line on the screen:

```
path   /api/v2/charge  →  /v2/charge
       rewrite ^/api/(.*) /$1 break                        api.conf:18
```

Unchanged paths render `unchanged` in muted text rather than being hidden. Absence would be ambiguous — the reader cannot tell whether the path was unchanged or whether we did not check.

### 4.2 The precedence explanation

Inline, next to the Route, in muted text. Not a tooltip.

> longest matching prefix; no `=` or `^~` location matched, and no regex location matched first

This is the sentence that stops the "your tool is wrong" conversation. Users disbelieve route ordering — correctly, having been burned by it — and a bare answer invites a challenge while an explanation pre-empts it and teaches the semantics at the same time. In a tooltip it does neither, because nobody hovers over an answer they already distrust.

### 4.3 Shadowed rules

Collapsed by default with a warning-coloured summary line, because it is a finding and finding-shaped things should not be silent:

```
⚠ 1 rule configured here is not in effect                              hide ▴
  ────────────────────────────────────────────────────────────────────────────
  add_header Strict-Transport-Security "max-age=31536000"    api.conf:9
    configured at   server scope
    discarded by    location /api declaring its own add_header
                                                             api.conf:21
    why             in nginx, an add_header in an inner block discards all
                    add_header directives inherited from enclosing blocks
    [ Verify with a probe ]
```

Both lines link to the file viewer. The `why` line is present because this rule is genuinely surprising and the operator's first reaction is that we are wrong.

The `Verify with a probe` button turns the analysis into proof. When the Probe returns and the header is absent, the block turns danger-coloured and reads **Disproved — configured but not served**, which is the single most compelling output the product produces.

### 4.4 Rules in effect

Collapsed, grouped by Action Class, each row: directive, arguments, class badge, confidence badge, scope, inherited-from, provenance link.

Inherited Rules are marked `inherited from server` in muted text. Unmodelled Rules show verbatim text with an `unmodelled` chip and the note *nagipath stores this directive but does not interpret it* — honest, and it prevents the operator assuming we silently ignored something.

### 4.5 The edge between Hops

Not a bare arrow. It carries the Upstream Member, the resolved addresses, **which Node resolved them**, and any flags:

```
│ be_payments → web02.corp.example:443
│ 10.20.1.12   resolved on lb01                              Verified
```

`resolved on lb01` matters more than it looks. Split-horizon DNS is normal, and an operator who sees an address they do not recognise needs to know whose view produced it. Two Nodes disagreeing about one name is a finding, and the edge links to it.

### 4.6 The External Hop

Distinct card treatment, not an error. Names the address, states the reason, and offers `Add this host as a node` — the External Hop is the most common way a fleet grows in nagipath, and turning a dead end into the next onboarding action is the right move at exactly that moment.

---

## 5. Undetermined branches

Footer block, warning-coloured, expanded on click:

```
⚠ 1 branch could not be evaluated                                     hide ▴
  hop 1   use_backend be_canary if { hdr_sub(cookie) canary=1 }
          haproxy.cfg:71
          the trace engine cannot evaluate this ACL expression, so this
          branch is neither followed nor ruled out
```

Verbatim text, provenance link, specific reason. Dropping these would make the Trace look cleaner and make it a lie — an operator debugging a canary would be shown a trace with no canary in it.

---

## 6. The graph view

Cytoscape.js, the only JavaScript in the product beyond ~400 lines of htmx glue.

Nodes are Instances (shaped by Vendor) and External Hops (dashed). Edges are labelled with Upstream → target and coloured by confidence. Directed left to right, dagre layout.

Interactions: click a node to open its Hop card in the right panel; click an edge for the Upstream Member detail; scroll to zoom; drag to pan; `Fit` and `Reset` buttons; `Export PNG`.

**The table view is the accessible equivalent, not a fallback.** With JS disabled or a screen reader active, the toggle is absent and the table is served. Every fact in the graph is in the table — the graph adds spatial intuition, never information.

---

## 7. Verification

`Verify` opens a confirm dialog. It has to, because this is the one action that leaves the process:

```
  Send a probe?

  nagipath will send one GET request to
      https://payments.corp.example/api/v2/charge
  from nagipath01.corp.example, then read the access log on each hop
  to find it.

  The request identifies itself as nagipath-probe/1.0 in its User-Agent.

  ⚠ If this URL has side effects even for GET requests, this will trigger
    them. nagipath cannot know whether it does.

  Readiness:  3 of 4 hops can be verified.
              hop 3 (app01, apache) — log format lacks %v

  [ Cancel ]                                            [ Send probe ]
```

Readiness is fetched before the dialog renders, so nobody is surprised by a partial result afterwards. The side-effect warning is on the screen, not in the manual.

**During** — Button becomes a spinner; a progress strip shows `requesting… reading logs on 4 instances… correlating…`. Typically 5–15 seconds, so staged messages rather than an opaque wait.

**After** — Hop badges update in place with a brief highlight. Each promoted badge gains an `evidence ▾` link:

```
Verified                                                        evidence ▴
  /var/log/haproxy.log
  Aug 21 09:31:02 lb01 haproxy[9912]: 10.1.2.3:52134
  [21/Aug/2026:09:31:02.114] fe_https~ be_payments/web02 0/0/1/12/13
  200 512 - - ---- 4/4/0/0/0 0/0 "GET /api/v2/charge HTTP/1.1"

  matched by  user-agent token
  frontend    fe_https      backend  be_payments      server  web02
```

Verbatim bytes, then the parsed fields. The raw line first, because that is what convinces.

**Partial** — Stated specifically, never as a bare "partial":

```
Verified (arrival only)                                                  ⓘ
  log_format 'main' lacks $server_name and $upstream_addr. Arrival at this
  instance is proven. Site selection and upstream choice remain inferred.
```

**Blocked** — Stays `Inferred`, with the fix:

```
Could not verify                                                         ⓘ
  the access log format lacks %v, so a request cannot be attributed to a
  specific vhost

  add this to the apache configuration and re-run the probe:
    LogFormat "%v %h %l %u %t \"%r\" %>s %b %f" nagipath      [ copy ]

  nagipath will not make this change for you.
```

That last line is deliberate. The moment the product offers to edit a customer's configuration it is no longer read-only, and read-only is what gets it installed.

---

## 8. Loading states

| Phase | Treatment |
|---|---|
| Cached, fresh | Immediate render. |
| Cached, stale | Immediate render of the stale result, `⚠ recomputing…` in the header, htmx swap when done. Never a spinner over data we already have. |
| Cold compute | Staged progress: `finding listeners for port 443… selecting sites for payments.corp.example… resolving upstream members… walking hop 3…`. A blank panel for five seconds reads as broken; naming the step reads as working. |
| Recompute | Existing Trace stays visible, dimmed, with an inline spinner. |
| Probe | Trace stays fully interactive; only badges are pending. |
| **Unreproducible** | The Snapshots this Trace walked have been pruned by retention (ADR-0015). Hop cards render their shape — Instance, path in and out, terminal reason — with the rules section replaced by `configuration no longer stored · collected 2026-05-02` and provenance links disabled rather than dead. A banner names the cause and offers `Retrace` as the primary action. Any Probe evidence stays fully visible and linked, because it survives retention. |

The unreproducible state gets its own row rather than folding into "stale" because the failure mode it prevents is specific: a Trace whose rule lists are silently empty looks like a Trace of a fleet with no rules. Empty Hop cards with no explanation is the one presentation worse than either option in ADR-0015.

---

## 9. Error and empty states

Every one of these is a real answer, not a failure, and the copy has to make that clear.

| State | What the user sees |
|---|---|
| Empty input | `Enter a URL, or pick an entry point.` Below it, three hostnames discovered during collection as one-click examples — the fastest path to a first result. |
| Malformed URL | Inline: `Enter a full URL including scheme, e.g. https://host/path`. |
| `no_matching_listener` | `No instance in this fleet listens on port 8443 with TLS.` Then the ports that *are* listening on Sites claiming that hostname. Usually a typo'd port, and this answers that immediately. |
| `no_matching_site` | `No site claims payments.corp.exmaple.` Then the nearest hostnames found. Answers "did I typo it" without the user having to suspect a typo. |
| `no_matching_route` | Site named, `no route matches /foo and no catch-all is configured`, with the Routes that do exist. |
| `unresolvable_upstream` | Verbatim target (`proxy_pass http://$backend`) and the explanation that the destination depends on runtime state. Honest, not an error. |
| `static_content` | `This route serves files from /var/www/payments and does not proxy.` A complete, legitimate answer — styled as success, because it is one. |
| `hop_limit` | `Truncated at 10 hops.` Framed as the finding: a real 10-hop request path is a problem worth reporting. |
| `loop_detected` | The repeating pair named, provenance for both. Presented as a misconfiguration in their fleet, not a tool limit. |
| Single-hop trace | Note: `this site is served directly; there is no proxy hop`. Prevents a correct answer reading as a failure — particularly on a first trace. |
| Degraded Snapshot on the path | Amber Hop border, `collected with gaps` chip, the specific gap named. |
| Unparsed Snapshot on the path | Trace **stops** with `cannot continue: configuration on app01 failed to parse` and the parser error, plus a link to browse the text. Refusing is correct; a truncated trace that looks complete is the worst possible output. |
| Retention removed a referenced Snapshot | Hop shape retained, detail rows show `no longer stored`, header notes the Trace cannot be fully reproduced. |
| HTTP entry that redirects to HTTPS | Redirect Rule shown, then a card: `this request is redirected to https://…` with `[ Trace that URL ]`. Two request paths, two Traces — conflating them would be wrong. |
| Two entry candidates | Both listed above hop 1, the unselected one with its reason and `[ Trace from here instead ]`. Hiding it would mislead. |
| `viewer` clicks Verify | Button disabled, hover: `probes require the admin role`. No dead-end dialog. |
| Probe transport failure | `nagipath could not reach this URL from nagipath01.corp.example` with the transport error, and the explicit note that this is a connectivity problem *here*, not a finding about their fleet. |
| Probe rate limited | `429` → `too many probes for this entry point; try again in 2 minutes`. |

---

## 10. Acceptance criteria

| # | Criterion |
|---|---|
| 1 | A URL in the input produces a Trace or a specific reason it could not, never a generic failure. |
| 2 | Table view is the default; the graph is a toggle. |
| 3 | Every Hop shows Instance, Node, Vendor, version, Site, Route, Upstream and confidence. |
| 4 | Every Route shows its precedence explanation inline, not in a tooltip. |
| 5 | The path row is always present, showing `unchanged` when it did not change. |
| 6 | Path changes name the Rule with a provenance link. |
| 7 | Every provenance link opens the Snapshot file viewer at the highlighted byte range. |
| 8 | Shadowed Rules are surfaced with the causing directive, both provenance links, and an explanation of the Vendor semantics. |
| 9 | Undetermined branches are shown verbatim with a reason. |
| 10 | External Hops name the address and reason and offer to add the host as a Node. |
| 11 | Terminal reason is always displayed in plain English. |
| 12 | Freshness is displayed on every Trace; stale results are served flagged rather than blocked. |
| 13 | Cold computation shows staged progress naming the current step. |
| 14 | Verify shows readiness and a side-effect warning before sending. |
| 15 | Every promoted confidence badge links to verbatim evidence. |
| 16 | Partial verification states exactly what is proven and what is not. |
| 17 | Blocked verification shows the exact directive and states nagipath will not apply it. |
| 18 | The graph view is fully equivalent to the table; the table is served when JS is unavailable. |
| 19 | Every Trace view is deep-linkable and exportable as Markdown. |
| 20 | A single-hop Trace explains that it is complete. |
| 21 | An unreproducible Trace names the cause, keeps its shape, disables rather than breaks provenance links, and offers `Retrace`. |
| 22 | Probe evidence on an unreproducible Trace remains visible and linked. |
| 23 | No Trace ever renders an empty rules section without stating why it is empty. |
| 24 | Trace of a 4-Hop path on a 40-Instance fleet renders in under 2s warm, under 5s cold. |
