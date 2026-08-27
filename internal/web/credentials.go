package web

import (
	"net/http"
	"strings"

	"github.com/nagiflow/nagipath/internal/store"
)

// ---------------------------------------------------------------- credentials

type credentialsData struct {
	List       []store.CredentialView
	AuthKinds  []string
	TypeFilter string
}

func (s *Server) credentials(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	typeFilter := r.URL.Query().Get("type")

	kinds, err := s.DB.CredentialAuthKinds(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	list, err := s.DB.CredentialsWithUsage(ctx, typeFilter)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	data := credentialsData{
		List:       list,
		AuthKinds:  kinds,
		TypeFilter: typeFilter,
	}
	s.render(w, r, "credentials.html", "Credentials", data)
}

func (s *Server) addCredential(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	name := strings.TrimSpace(r.FormValue("name"))
	kind := r.FormValue("auth_kind")
	if kind == "" {
		kind = "private_key"
	}
	username := strings.TrimSpace(r.FormValue("username"))
	if name == "" {
		redirect(w, r, "/settings/credentials", "", "a credential name is required")
		return
	}

	var (
		id  int64
		err error
	)
	switch kind {
	case "private_key", "ssh_certificate":
		key := r.FormValue("private_key")
		if strings.TrimSpace(key) == "" {
			redirect(w, r, "/settings/credentials", "", "a private key is required")
			return
		}
		if kind == "ssh_certificate" && strings.TrimSpace(r.FormValue("certificate")) == "" {
			redirect(w, r, "/settings/credentials", "", "an SSH certificate is required")
			return
		}
		id, err = s.DB.CreateCredential(ctx, s.Master, name, username, kind, key,
			r.FormValue("passphrase"), r.FormValue("certificate"), &u.ID)
	case "username_password", "ldap", "kerberos":
		id, err = s.DB.CreatePasswordCredential(ctx, s.Master, name, username, kind,
			r.FormValue("password"), strings.TrimSpace(r.FormValue("external_ref")), &u.ID)
	case "cyberark":
		id, err = s.DB.CreateCyberArkCredential(ctx, name, username, strings.TrimSpace(r.FormValue("external_ref")), &u.ID)
	default:
		redirect(w, r, "/settings/credentials", "", "choose a supported credential type")
		return
	}
	if err != nil {
		redirect(w, r, "/settings/credentials", "", err.Error())
		return
	}
	// Label with the name only — never the key material — matching what the
	// credentials list itself displays.
	s.DB.Audit(ctx, &u.ID, "credential.create", "credential", &id, name)
	// The key is now encrypted at rest and is never rendered again, by any route.
	redirect(w, r, "/settings/credentials", "credential stored", "")
}
