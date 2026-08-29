package api

import (
	"net/http"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// protoMarshal is protojson's canonical camelCase JSON mapping — the wire
// format every response in this package now speaks, matched on the frontend
// by @bufbuild/protobuf's fromJson against the same generated schema
// (buf.gen.yaml, proto/nagipath/api/v1/*.proto). EmitUnpopulated matches
// encoding/json's default of always including zero-value fields, so a
// TypeScript consumer doesn't have to treat "absent" and "zero" as the same
// thing when they aren't (see dashboard.go's []T{} vs nil discussion).
var protoJSON = protojson.MarshalOptions{EmitUnpopulated: true}

func writeProto(w http.ResponseWriter, status int, msg proto.Message) {
	body, err := protoJSON.Marshal(msg)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "encode_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
