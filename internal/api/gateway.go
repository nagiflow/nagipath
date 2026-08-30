package api

import (
	"context"
	"net/http"
	"strings"
	"unicode"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

// newGateway is one *runtime.ServeMux per domain service (ClusterService,
// and every future one converted the same way) — grpc-gateway's own path
// matching is per-mux, so domains don't share one and step on each other's
// patterns. Same wire format as writeProto's protoJSON (proto.go): the
// frontend's fromJson doesn't care whether a response crossed a plain
// http.HandlerFunc or a gateway.
func newGateway() *runtime.ServeMux {
	return runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions:   protoJSON,
			UnmarshalOptions: protojson.UnmarshalOptions{DiscardUnknown: true},
		}),
		runtime.WithErrorHandler(gatewayError),
		runtime.WithOutgoingHeaderMatcher(outgoingHeaderMatcher),
	)
}

// outgoingHeaderMatcher is grpc.SetHeader's route back to a real HTTP
// response header. The default matcher forwards everything with a
// Grpc-Metadata- prefix — harmless noise for every RPC in this package
// except sessionservice.go's Set-Cookie, which a browser only honours
// unprefixed, so that one key is passed through as-is and nothing else is
// forwarded at all (no RPC here needs a second outgoing header, and
// grpc-metadata-* on every response would just be clutter).
func outgoingHeaderMatcher(key string) (string, bool) {
	if key == "set-cookie" {
		return "Set-Cookie", true
	}
	return "", false
}

// gatewayError replaces grpc-gateway's default {"code","message","details"}
// error body with this package's existing {"error":{"code","message"}}
// envelope (middleware.go's apiError) — every non-gateway handler in this
// package still uses it, and the frontend's APIError only knows that shape.
// The machine-readable code is the gRPC status code's own name
// (permission_denied, invalid_argument, ...): coarser than the bespoke
// per-handler codes elsewhere in this package (rename_failed,
// datastore_unavailable), since a codes.Code is all a status.Error carries
// without extra machinery (google.golang.org/genproto's errdetails).
// ponytail: fine while nothing branches on these codes client-side; add
// errdetails.ErrorInfo on individual status.Errors if a specific one needs
// its old string back.
func gatewayError(ctx context.Context, mux *runtime.ServeMux, marshaler runtime.Marshaler, w http.ResponseWriter, r *http.Request, err error) {
	st := status.Convert(err)
	apiError(w, httpStatusFromCode(st.Code()), snakeCase(st.Code().String()), st.Message())
}

// httpStatusFromCode is runtime.HTTPStatusFromCode with one override:
// codes.InvalidArgument maps to 422, not 400. Every status.Error(codes.
// InvalidArgument, ...) in this package's *service.go files is a semantic
// validation failure (a password too short, a retention window out of
// range) — api_conventions.md's 422 ("well-formed but semantically
// invalid"), not its 400 ("malformed request"). A genuinely malformed JSON
// body never reaches a service method at all — grpc-gateway's own generated
// decode step rejects it first, with the same InvalidArgument code, so it
// gets 422 too; that one distinction (malformed body vs. bad value) wasn't
// worth two gRPC codes to preserve, and no test depends on it.
func httpStatusFromCode(code codes.Code) int {
	if code == codes.InvalidArgument {
		return http.StatusUnprocessableEntity
	}
	return runtime.HTTPStatusFromCode(code)
}

// gatewayOrCSV lets a domain keep its ?export=csv branch (sites.go and
// friends) outside the gateway — google.api.http routes by path+method, not
// query string, and a CSV download was never RPC-shaped data in the first
// place (ADR-0018) — while every other request on the same path goes
// through the gateway mux.
func gatewayOrCSV(gw http.Handler, csv http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("export") == "csv" {
			csv(w, r)
			return
		}
		gw.ServeHTTP(w, r)
	}
}

// snakeCase turns a gRPC codes.Code's CamelCase String() (PermissionDenied,
// InvalidArgument) into this package's snake_case machine codes
// (permission_denied, invalid_argument) — apiError's existing convention.
func snakeCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
