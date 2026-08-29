# The SPA's API is defined once in protobuf, generating both the Go and TypeScript types

[ADR-0017](0017-react-spa-with-eui-supersedes-0013.md) started `internal/api` with hand-written
Go response structs mirrored by hand-written TypeScript interfaces in `ui/src/api/types.ts` — one
schema maintained twice, by hand, in two languages. That drifted almost immediately: a field
renamed in `internal/api/sites.go` had no compiler-enforced link to the matching field in
`ui/src/api/types.ts`, and the mismatch would only surface at runtime, in a browser.

## Decision

Every `internal/api` request/response shape is defined once, in `.proto` files under `proto/`,
and generated into both `internal/api/pb/` (Go, via `protoc-gen-go`) and `ui/src/api/pb/`
(TypeScript, via `protoc-gen-es`/Protobuf-ES) by `buf generate`. Go handlers build the generated
message types directly; `internal/api/proto.go`'s `writeProto` marshals them with `protojson`
(camelCase JSON, the same wire format either side reads). The frontend's `api.get`/`api.post`
(`ui/src/api/client.ts`) decode with `fromJson` against the same generated schema, so a field
rename in one `.proto` file breaks the build on both sides instead of failing silently in a
browser.

Simple fire-and-forget mutations (a cluster rename, a host-key decision) stay a bare
`{"ok": true}`-shaped JSON envelope via `api.postAction` — not every request needs a schema, only
ones with a real response shape worth keeping in sync.

`buf`, `protoc`, `protoc-gen-go` are local machine tools (not vendored); `protoc-gen-es` is a dev
dependency of `ui/` via bun. Generated output (`internal/api/pb/`, `ui/src/api/pb/`) is committed,
the same "committed generated artifact" pattern [ADR-0016](0016-tailwind-build-step-with-generated-css-committed.md)
and ADR-0017 already established for `app.css` and `ui/dist` — `go build` still needs nothing but
Go, and `bun run build` still needs nothing but bun, even without `buf` installed.

## What does not change

Still one binary, still `/api/ui`'s session-cookie auth (protojson is just a wire format, not a
transport or auth change). The existing external Bearer-token API (`/api/v1/nodes|clusters|drift`,
`internal/web/api.go`) is untouched — it predates this ADR, stays hand-rolled JSON, and is not
folded into the proto schema here. If and when that API is opened to third parties and needs
published documentation, the right tool for that is OpenAPI (a spec generated from or alongside
its existing JSON contract), not protobuf — protobuf's natural fit is a binary RPC transport
(gRPC), and adopting it for `/api/v1` would mean asking external consumers to use gRPC tooling
instead of `curl`, a materially bigger decision than "keep two type definitions in sync" and out of
scope here.

## Consequences

**Every internal/api response now goes through a conversion step** from `store`/`trace` domain
types to generated proto messages (see `internal/api/dashboard.go`'s `dashboardBuild.toProto()`
for the pattern) — proto messages are their own generated Go types, not something `store.Node` can
satisfy directly the way it could sit inside a hand-written JSON struct. This is real code, but it
is the same amount of field-mapping code a hand-written DTO already required; nothing got smaller,
but the TypeScript side of the mapping is now generated instead of hand-typed and desyncable.

**int64 fields are `bigint` in TypeScript**, not `number` — protojson serializes 64-bit integers as
JSON strings to avoid precision loss, and Protobuf-ES follows the same convention. IDs used as
React keys or in template strings need an explicit `.toString()`; this is a real ergonomic cost,
accepted because IDs are exactly the field most likely to matter for correctness at scale.

**Editing an endpoint's shape means editing a `.proto` file and running `make proto`** before the
Go and TS sides will compile against it — one more command in the workflow ADR-0017 already
introduced with `make ui`, not a new category of toolchain dependency.
