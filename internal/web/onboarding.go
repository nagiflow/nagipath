package web

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/nagiflow/nagipath/internal/store"
)

// ---------------------------------------------------------------- onboarding

type onboardData struct {
	Step        int
	Nodes       []store.Node
	Credentials []store.Credential
	Pending     []store.PendingHostKey
	Collections []store.Collection
	// Imported and Refused are the result of the last inventory paste. Refused is
	// never empty-and-silent: a dropped host is a host that goes uncollected.
	Imported []string
	Refused  []string
	Source   string
	// The Master Key's path and whether it is there. Onboarding tells the operator
	// to back this file up, so it states where it actually is — it used to print a
	// hardcoded path and an unconditional "LOADED" badge, which is exactly the
	// claim you cannot get wrong on the screen that asks for a private key.
	MasterKeyPath    string
	MasterKeyPresent bool
	// GroupedKeys groups pending host keys by shared fingerprint. Several nodes
	// presenting one key is the fact that matters, and approving them separately
	// is busywork.
	GroupedKeys []fingerprintGroup
}

// fingerprintGroup is one fingerprint with all the nodes that present it.
type fingerprintGroup struct {
	Fingerprint string
	Algorithm   string
	Keys        []store.PendingHostKey
}

// onboarding is the four-step first-run path: name the nodes, give them a
// credential, approve their host keys by hand, collect once. Nothing runs on a
// node before step three, and step three has no automatic answer.
func (s *Server) onboarding(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := onboardData{Source: r.URL.Query().Get("source")}
	if d.Source != "ini" && d.Source != "yaml" {
		d.Source = "list"
	}
	d.Nodes, _ = s.DB.Nodes(ctx)
	d.Credentials, _ = s.DB.Credentials(ctx)
	d.Pending, _ = s.DB.PendingHostKeys(ctx)
	d.Collections, _ = s.DB.Collections(ctx, 5)
	d.MasterKeyPath = s.Master.Path
	_, err := os.Stat(s.Master.Path)
	d.MasterKeyPresent = err == nil
	d.Step = onboardStep(d)

	// Group pending keys by fingerprint
	d.GroupedKeys = groupKeysByFingerprint(d.Pending)

	s.render(w, r, "onboarding.html", "Connect the fleet", d)
}

// onboardStep is derived from state, never stored: an operator who leaves and
// comes back lands where the fleet actually is, not where a wizard remembers.
func onboardStep(d onboardData) int {
	switch {
	case len(d.Nodes) == 0:
		return 1
	case len(d.Credentials) == 0:
		return 2
	case len(d.Pending) > 0:
		return 3
	default:
		return 4
	}
}

// groupKeysByFingerprint groups pending host keys by shared fingerprint. Several
// nodes presenting one key is the fact that matters, and approving them separately
// is busywork.
func groupKeysByFingerprint(pending []store.PendingHostKey) []fingerprintGroup {
	groups := make(map[string]*fingerprintGroup)
	var order []string

	for _, k := range pending {
		if _, ok := groups[k.Fingerprint]; !ok {
			groups[k.Fingerprint] = &fingerprintGroup{
				Fingerprint: k.Fingerprint,
				Algorithm:   k.Algorithm,
			}
			order = append(order, k.Fingerprint)
		}
		groups[k.Fingerprint].Keys = append(groups[k.Fingerprint].Keys, k)
	}

	var out []fingerprintGroup
	for _, fp := range order {
		out = append(out, *groups[fp])
	}
	return out
}

// onboardNodes imports a pasted list or Ansible inventory. It is bulk entry, not
// discovery: every host in the text was named by a person, and anything that
// would expand into hosts nobody named is refused with a reason.
func (s *Server) onboardNodes(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	hosts, refused := parseInventory(r.FormValue("inventory"))
	if len(hosts) == 0 && len(refused) == 0 {
		redirect(w, r, "/onboarding", "", "no hosts in that text")
		return
	}
	var credID *int64
	if v, err := strconv.ParseInt(r.FormValue("credential_id"), 10, 64); err == nil && v > 0 {
		credID = &v
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
			"inventory", &u.ID); err != nil {
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
		redirect(w, r, "/onboarding", "", msg+" — refused: "+strings.Join(refused, "; "))
		return
	}
	redirect(w, r, "/onboarding", msg, "")
}

// approveHostKeys approves the keys the operator ticked. Each one is still an
// explicit decision on a specific fingerprint; there is no "trust everything"
// and no first-use shortcut anywhere in the product.
func (s *Server) approveHostKeys(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	// This form is on the wizard and on Settings · Host keys. Without honouring
	// `back`, an admin approving a rekey from Settings was dropped into the
	// first-run wizard.
	back := backTo(r, "/onboarding")
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
