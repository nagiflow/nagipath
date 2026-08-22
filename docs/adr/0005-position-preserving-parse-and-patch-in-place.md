# Parsers preserve byte positions, because editing will patch in place and never generate

Every Config Object and Rule records Provenance: the file and byte range it was parsed from. Parsers therefore produce a lossless, position-preserving tree rather than a tidy abstract one, from the first parser onwards — even though v1 writes nothing.

This exists because of how editing must eventually work. Every customer's configuration has a different file layout, naming convention, include structure and house style, so nagipath will never *generate* configuration. An edit will locate the object's byte range in its Snapshot, replace only those bytes, write the file back to the same path atomically, and let the Vendor's own validator arbitrate. Comments, indentation, ordering and every Opaque Directive survive untouched, because the file is mutated rather than regenerated. That is how a refactoring tool behaves, and it is the opposite of the template-and-own model that makes Ansible and Puppet fight each other over the same file.

Recording byte offsets while parsing is nearly free when designed in from the start, and a rewrite when bolted on afterwards. That asymmetry is the whole reason this is decided before parser one.

## Consequences

Provenance pays off immediately in v1: the diff viewer and the Rule lookup can both jump from a semantic element to its exact source file and line.

Creation of new objects is the one case that must emit text, and its mechanism is deliberately open — see `docs/OPEN-DECISIONS.md`.
