# An Application is a set of Entry Points, not a set of servers

An Application is a name, an owner, and a set of Entry Points — hostnames with optional path prefixes. Its Instances, Sites, Upstreams and Certificates are *derived* by tracing from those Entry Points and cached, never enumerated by hand.

Modelling an Application as a declared set of servers was rejected for three reasons: the Trace is already entry-point-driven, so the derivation is free; a declared server list goes stale the moment topology changes, whereas a derived one cannot; and it makes the data model match the mental model of the audience that matters most, since a developer thinks of their application as `payments.example.com/api` rather than as a list of hostnames they have never logged into.

## Consequences

A future reader will notice that Application has no membership table and may try to add one. Don't — the derivation is the point.

Candidate groupings can be proposed automatically from shared server-name suffixes or shared Upstreams, and seeded from imported Ansible groups, but inference only ever proposes; the operator confirms.
