// Package parse turns captured configuration text into the derived rows the rest
// of nagipath reasons about: Listeners, Sites, Routes, Upstreams, Members and
// Rules.
//
// Two rules shape everything here (ADR-0005):
//
//   - **Every derived row keeps its byte range.** A row without provenance is an
//     assertion; a row with provenance is evidence. The parse is
//     position-preserving, so a Rule can always be shown as the exact bytes that
//     produced it.
//
//   - **Nothing is dropped.** A directive we do not model becomes a Rule with
//     `is_modelled = 0` and its verbatim text. Silently discarding a line is how
//     an inventory ends up confidently wrong.
package parse

// Prov is a byte range inside one captured file. Paths here are the target's
// paths; the persister maps them to snapshot_file ids.
type Prov struct {
	Path  string
	Start int
	End   int
}

// Precedence ranks. Lower wins. These are the numbers the Trace engine orders by,
// and they are deliberately spread out so a vendor can gain a tier without a
// migration.
const (
	RankNginxExact      = 10 // location = /path
	RankNginxPrefixNoRE = 20 // location ^~ /path — a short-circuit, not a score
	RankNginxRegex      = 30 // location ~ / ~*  — resolved by FILE ORDER, not length
	RankNginxPrefix     = 40 // location /path
	RankApacheDirectory = 50
	RankApacheDirMatch  = 60
	RankApacheFiles     = 70
	RankApacheLocation  = 80 // <Location> beats <Directory>
	RankHAProxyUseBack  = 90
	RankHAProxyDefault  = 99
)

// Action classes. Closed set (schema.md); `other` is the honest bucket, paired
// with is_modelled = 0.
const (
	ClassMatch         = "match"
	ClassRewrite       = "rewrite"
	ClassRedirect      = "redirect"
	ClassHeader        = "header"
	ClassAuth          = "auth"
	ClassCache         = "cache"
	ClassRateLimit     = "rate_limit"
	ClassProxy         = "proxy"
	ClassAccessControl = "access_control"
	ClassOther         = "other"
)

type Listener struct {
	Prov
	NaturalKey string
	Ordinal    int
	Address    string
	Port       int
	TLS        bool
	Protocol   string
	IsDefault  bool
	Raw        string
}

type SiteName struct {
	Name      string
	MatchKind string // exact | wildcard_prefix | wildcard_suffix | regex | catch_all
	Ordinal   int
}

type Site struct {
	Prov
	NaturalKey   string
	Ordinal      int
	ListenerKeys []string
	PrimaryName  string
	Kind         string // nginx_server | apache_vhost | haproxy_frontend
	DocumentRoot string
	Raw          string
	Names        []SiteName
	Routes       []*Route
	Rules        []*Rule
}

type Route struct {
	Prov
	NaturalKey     string
	Ordinal        int
	MatchType      string
	Pattern        string
	PrecedenceRank int
	Specificity    int
	UpstreamKey    string // natural key of the Upstream this route sends to
	TargetRaw      string
	IsTerminal     bool
	Raw            string
	Parent         *Route
	Routes         []*Route
	Rules          []*Rule
}

type Upstream struct {
	Prov
	NaturalKey    string
	Ordinal       int
	Name          string
	Kind          string
	BalanceMethod string
	Raw           string
	Members       []*Member
}

type Member struct {
	Prov
	NaturalKey string
	Ordinal    int
	Host       string
	Port       int
	Scheme     string
	Weight     int
	Flags      string
	Raw        string
}

type Rule struct {
	Prov
	NaturalKey  string
	Ordinal     int
	ScopeKind   string // global | http | site | route | upstream | listener
	Directive   string
	ActionClass string
	Args        string
	Raw         string
	IsModelled  bool
	// Shadowed marks a Rule that is configured but cannot take effect, because a
	// nearer scope discards the whole inherited set. NGINX `add_header` is the
	// canonical case and the product's single most valuable output.
	Shadowed   bool
	ShadowedBy string
}

type CertBinding struct {
	Prov
	SiteKey     string
	CertPath    string
	KeyPath     string
	ChainPath   string
	CombinedPEM bool
}

// Result is one Snapshot's worth of derived rows.
type Result struct {
	Vendor         string
	Listeners      []*Listener
	Sites          []*Site
	Upstreams      []*Upstream
	GlobalRules    []*Rule
	CertBindings   []*CertBinding
	Warnings       []string
	Degraded       bool
	DegradedReason string
}

// File is one captured configuration file.
type File struct {
	Path    string
	Content []byte
}

func (r *Result) warn(msg string) {
	r.Warnings = append(r.Warnings, msg)
}

// degrade records that the parse is incomplete but usable. A degraded parse is
// still shown, labelled; a failed one is not shown at all.
func (r *Result) degrade(reason string) {
	r.Degraded = true
	if r.DegradedReason == "" {
		r.DegradedReason = reason
	} else {
		r.DegradedReason += "; " + reason
	}
}
