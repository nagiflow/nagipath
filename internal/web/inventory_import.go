package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/nagiflow/nagipath/internal/store"
)

type inventoryImportData struct {
	Credentials []store.Credential
}

func (s *Server) importNodesPage(w http.ResponseWriter, r *http.Request) {
	d := inventoryImportData{}
	d.Credentials, _ = s.DB.Credentials(r.Context())
	s.render(w, r, "inventory_import.html", "Import inventory", d)
}

// importNodes is bulk entry, not discovery: every host in the text was named by
// a person, and anything that would expand into hosts nobody named is refused.
func (s *Server) importNodes(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	hosts, refused := parseInventory(r.FormValue("inventory"))
	if len(hosts) == 0 && len(refused) == 0 {
		redirect(w, r, "/nodes/import", "", "no hosts in that text")
		return
	}
	var credID *int64
	if v, err := strconv.ParseInt(r.FormValue("credential_id"), 10, 64); err == nil && v > 0 {
		credID = &v
	}
	if credID == nil {
		redirect(w, r, "/nodes/import", "", "select a credential before adding nodes")
		return
	}
	var bastion *int64
	if v, err := strconv.ParseInt(r.FormValue("bastion_id"), 10, 64); err == nil && v > 0 {
		bastion = &v
	}
	added := 0
	for _, h := range hosts {
		user := h.User
		if user == "" {
			user = strings.TrimSpace(r.FormValue("username"))
		}
		if _, err := s.DB.AddNode(ctx, h.Address, h.Port, h.Name, user, credID, bastion,
			"ansible_inventory", &u.ID); err != nil {
			refused = append(refused, h.label()+": "+err.Error())
			continue
		}
		added++
	}
	s.DB.AuditDetail(ctx, &u.ID, "node.import", "node", nil,
		fmt.Sprintf("%d added, %d refused", added, len(refused)),
		map[string]any{"added": added, "refused": refused}, "success", remoteAddr(r))
	msg := fmt.Sprintf("%d node(s) added; collect each one to fetch its host key", added)
	if len(refused) > 0 {
		redirect(w, r, "/nodes/import", "", msg+" — refused: "+strings.Join(refused, "; "))
		return
	}
	redirect(w, r, "/nodes/import", msg, "")
}

func (s *Server) approveHostKeys(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	back := backTo(r, "/settings/hostkeys")
	ids := r.Form["key"]
	if len(ids) == 0 {
		redirect(w, r, back, "", "no host key was selected")
		return
	}
	approve := r.FormValue("decision") != "reject"
	n := 0
	for _, raw := range ids {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			continue
		}
		if err := s.DB.DecideHostKey(ctx, id, approve, &u.ID); err != nil {
			redirect(w, r, back, "", err.Error())
			return
		}
		n++
	}
	word := "approved"
	if !approve {
		word = "rejected"
	}
	redirect(w, r, back, fmt.Sprintf("%d host key(s) %s", n, word), "")
}
