# v1 is read-only, and never scans networks

v1 collects, parses and reports. It writes nothing to any managed host: no change sets, no validation orchestration, no canary batches, no rolling deployment, no rollback. The reason is blast radius — a defect in a read-only tool produces a wrong screen, while a defect in a write path takes down someone's payment site — and it removes the hardest security-review conversation, because nagipath needs no write credentials at all. Certificate expiry and undocumented request paths are already a fundable problem on their own.

nagipath also never scans networks. Nodes are supplied by the operator as a host list or an imported Ansible inventory; CIDR discovery scanning was rejected because unannounced port sweeps inside an enterprise produce a security incident rather than a customer. "nagipath only connects to hosts you list" is a stated product property, not an incidental limitation.

## Consequences

The single exception to read-only is the Probe: an operator-initiated GET or HEAD request used to verify a Trace. It is never scheduled, never automatic, capped in redirect depth, audited with actor and target, and labelled with the host it originated from — because source-IP policy, WAF rules or a different ingress can route it differently than a real user's request.

Editing is no longer undecided: [ADR-0020](0020-operator-initiated-edit-and-restart.md) adds two
operator-initiated writes — edit one file, restart one Instance — and keeps everything else in this
ADR's list rejected.
