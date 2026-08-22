# DNS names in configuration are resolved on the target host, not by us

Connecting an Upstream Member such as `proxy_pass https://payments-be.internal` to a managed Instance requires resolving that name. It is resolved **on the Node**, via `getent hosts` over the SSH session we already hold, and cached briefly per (Node, name).

Resolving locally with our own resolver was rejected because split-horizon DNS is normal in segmented enterprises: the web server's view of a name and nagipath's view can legitimately differ, and the web server's is the only one that determines where traffic actually goes. A future reader will see a remote command where a standard library lookup would do and should not "fix" it.

## Consequences

Because our own resolver's view is available incidentally, disagreement between the two is surfaced as an informational finding — split-horizon misconfiguration is itself a cause of outages, so the discrepancy is worth reporting rather than discarding.
