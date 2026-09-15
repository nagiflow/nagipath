package api

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	_ "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
)

var pathParam = regexp.MustCompile(`\{[^}]*\}`)

// Every google.api.http rule in the protos needs a matching route on the mux:
// the gateways are registered per-route, not wholesale, so a new rpc whose
// route nobody added answers 405 (its path still matches a GET-only pattern)
// instead of running. That is exactly how POST /nodes/{id}/test shipped dead.
func TestEveryHTTPRuleIsRouted(t *testing.T) {
	s := New(nil, nil, false)
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !strings.HasPrefix(string(fd.Package()), "nagipath.api.") {
			return true
		}
		for i := 0; i < fd.Services().Len(); i++ {
			ms := fd.Services().Get(i).Methods()
			for j := 0; j < ms.Len(); j++ {
				m := ms.Get(j)
				rule, _ := proto.GetExtension(m.Options(), annotations.E_Http).(*annotations.HttpRule)
				if rule == nil {
					continue
				}
				method, path := "GET", rule.GetGet()
				if p := rule.GetPost(); p != "" {
					method, path = "POST", p
				}
				if path == "" {
					continue
				}
				req := httptest.NewRequest(method, pathParam.ReplaceAllString(path, "1"), nil)
				if _, pattern := s.mux.Handler(req); pattern == "" {
					t.Errorf("%s: no route for %s %s (add it in api.go)", m.FullName(), method, path)
				}
			}
		}
		return true
	})
}
