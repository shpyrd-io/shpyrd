package store

import (
	"context"
	"errors"
	"fmt"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Memory is the in-memory Store: tests, and the reference for the SQL one.
type tokenEntry struct {
	token       APIToken
	hash        string
	lastUpdated time.Time
}

type invitationEntry struct {
	inv  Invitation
	hash string
}

type Memory struct {
	mu          sync.Mutex
	workspaces  map[string]*Workspace // by slug
	identities  []Identity
	teams       []Team
	grants      []Grant
	sessions    map[string]*Session
	codes       map[string]*Code
	domains     []DomainClaim
	tokens      []tokenEntry
	memberships []Membership
	invitations []invitationEntry
	hosts       []WorkspaceHost
	oclients    []OAuthClient
	ocodes      map[string]*OAuthCode
	otokens     []OAuthToken
	bill        *billingMemory
	now         func() time.Time
}

// NewMemory returns an empty store with the implicit workspace.
func NewMemory() *Memory {
	m := &Memory{workspaces: map[string]*Workspace{}, sessions: map[string]*Session{}, codes: map[string]*Code{}, now: func() time.Time { return time.Now().UTC() }}
	_ = m.Migrate(context.Background(), "shpyrd")
	return m
}

func newID() string { return uuid.NewString() }

func (m *Memory) Migrate(_ context.Context, defaultName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workspaces[DefaultWorkspace]; !ok {
		t := m.now()
		m.workspaces[DefaultWorkspace] = &Workspace{ID: newID(), Slug: DefaultWorkspace, Name: defaultName, Status: WorkspaceActive, CreatedAt: t, UpdatedAt: t}
	}
	w := m.workspaces[DefaultWorkspace]
	found := false
	for _, t := range m.teams {
		if t.WorkspaceID == w.ID && t.Everyone {
			found = true
		}
	}
	if !found {
		t := m.now()
		m.teams = append(m.teams, Team{ID: newID(), WorkspaceID: w.ID, Name: TeamEveryone, Description: "Everyone who has signed in", Members: []string{}, Groups: []string{}, Everyone: true, CreatedAt: t, UpdatedAt: t})
	}
	return nil
}

func (m *Memory) Close() {}

// hasWorkspaceID reports whether id is the id of a known workspace.
func (m *Memory) hasWorkspaceID(id string) bool {
	for _, w := range m.workspaces {
		if w.ID == id {
			return true
		}
	}
	return false
}

func (m *Memory) ws(slug string) (*Workspace, error) {
	w, ok := m.workspaces[slug]
	if !ok {
		return nil, ErrNotFound
	}
	return w, nil
}

func (m *Memory) Workspace(_ context.Context, slug string) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(slug)
	if err != nil {
		return nil, err
	}
	c := *w
	return &c, nil
}

func (m *Memory) WorkspaceByAddress(_ context.Context, address string) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	address = strings.ToLower(strings.TrimSpace(address))
	if address == "" {
		return nil, ErrNotFound
	}
	for _, w := range m.workspaces {
		if w.Address == address {
			c := *w
			return &c, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) ListWorkspaces(_ context.Context) ([]Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Workspace, 0, len(m.workspaces))
	for _, w := range m.workspaces {
		out = append(out, *w)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].Slug < out[j].Slug
	})
	return out, nil
}

func (m *Memory) CreateWorkspace(_ context.Context, w Workspace) (*Workspace, error) {
	if err := project.ValidateWorkspaceSlug(w.Slug); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	w.Address = strings.ToLower(strings.TrimSpace(w.Address))
	if _, taken := m.workspaces[w.Slug]; taken {
		return nil, ErrConflict
	}
	for _, have := range m.workspaces {
		if w.Address != "" && have.Address == w.Address {
			return nil, ErrConflict
		}
	}
	for _, h := range m.hosts {
		if w.Address != "" && h.Host == w.Address {
			return nil, ErrConflict // a custom domain or a moved address of another workspace
		}
	}
	t := m.now()
	w.ID, w.CreatedAt, w.UpdatedAt = newID(), t, t
	if w.Status == "" {
		w.Status = WorkspaceActive
	}
	c := w
	m.workspaces[w.Slug] = &c
	m.teams = append(m.teams, Team{ID: newID(), WorkspaceID: c.ID, Name: TeamEveryone, Description: "Everyone who has signed in", Members: []string{}, Groups: []string{}, Everyone: true, CreatedAt: t, UpdatedAt: t})
	out := c
	return &out, nil
}

func (m *Memory) SetWorkspaceStatus(_ context.Context, slug, status string) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(slug)
	if err != nil {
		return nil, err
	}
	w.Status, w.UpdatedAt = status, m.now()
	c := *w
	return &c, nil
}

func (m *Memory) UpdateWorkspace(_ context.Context, slug, name string) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(slug)
	if err != nil {
		return nil, err
	}
	w.Name, w.UpdatedAt = name, m.now()
	c := *w
	return &c, nil
}

func (m *Memory) UpdateWorkspaceSettings(_ context.Context, slug string, settings WorkspaceSettings) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(slug)
	if err != nil {
		return nil, err
	}
	w.Settings, w.UpdatedAt = settings, m.now()
	c := *w
	return &c, nil
}

func (m *Memory) ListDomainClaims(_ context.Context, ws string) ([]DomainClaim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	var out []DomainClaim
	for _, d := range m.domains {
		if d.WorkspaceID == w.ID {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out, nil
}

func (m *Memory) PutDomainClaim(_ context.Context, ws, domain, connector string) (*DomainClaim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	for i := range m.domains {
		d := &m.domains[i]
		if d.WorkspaceID == w.ID && d.Domain == domain {
			d.Connector = connector
			c := *d
			return &c, nil
		}
	}
	d := DomainClaim{ID: newID(), WorkspaceID: w.ID, Domain: domain, Token: newID(), Connector: connector, CreatedAt: m.now()}
	m.domains = append(m.domains, d)
	return &d, nil
}

func (m *Memory) MarkDomainVerified(_ context.Context, ws, domain string, at time.Time) (*DomainClaim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	for i := range m.domains {
		d := &m.domains[i]
		if d.WorkspaceID == w.ID && d.Domain == strings.ToLower(domain) {
			t := at
			d.VerifiedAt = &t
			c := *d
			return &c, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) DeleteDomainClaim(_ context.Context, ws, domain string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return err
	}
	for i, d := range m.domains {
		if d.WorkspaceID == w.ID && d.Domain == strings.ToLower(domain) {
			m.domains = append(m.domains[:i], m.domains[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func (m *Memory) TouchIdentity(_ context.Context, ws string, id Identity) (*Identity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	email := strings.ToLower(strings.TrimSpace(id.Email))
	realm := id.Realm
	if realm == "" {
		realm = "workspace"
	}
	now := m.now()
	for i := range m.identities {
		it := &m.identities[i]
		if it.WorkspaceID == w.ID && it.Realm == realm && it.Email == email {
			if id.Name != "" {
				it.Name = id.Name
			}
			if id.Provider != "" {
				it.Provider = id.Provider
			}
			if id.Groups != nil {
				it.Groups = append([]string(nil), id.Groups...)
			}
			it.LastSeenAt = now
			c := *it
			return &c, nil
		}
	}
	it := Identity{ID: newID(), WorkspaceID: w.ID, Realm: realm, Email: email, Name: id.Name, Provider: id.Provider, Groups: append([]string(nil), id.Groups...), Status: StatusActive, FirstSeenAt: now, LastSeenAt: now}
	m.identities = append(m.identities, it)
	return &it, nil
}

func (m *Memory) ListIdentities(_ context.Context, ws string) ([]Identity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	var out []Identity
	for _, it := range m.identities {
		if it.WorkspaceID == w.ID {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

func (m *Memory) GetIdentity(_ context.Context, ws, email string) (*Identity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	for _, it := range m.identities {
		if it.WorkspaceID == w.ID && strings.EqualFold(it.Email, email) {
			c := it
			return &c, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) SetIdentityStatus(_ context.Context, ws, email, status string) (*Identity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	email = strings.ToLower(email)
	for i := range m.identities {
		it := &m.identities[i]
		if it.WorkspaceID == w.ID && it.Email == email {
			it.Status = status
			c := *it
			return &c, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) DeleteIdentity(_ context.Context, ws, email string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return err
	}
	email = strings.ToLower(email)
	for i, it := range m.identities {
		if it.WorkspaceID == w.ID && it.Email == email {
			m.identities = append(m.identities[:i], m.identities[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func (m *Memory) ListTeams(_ context.Context, ws string) ([]Team, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	var out []Team
	for _, t := range m.teams {
		if t.WorkspaceID == w.ID {
			out = append(out, cloneTeam(t))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *Memory) GetTeam(_ context.Context, ws, name string) (*Team, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	for _, t := range m.teams {
		if t.WorkspaceID == w.ID && t.Name == name {
			c := cloneTeam(t)
			return &c, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) PutTeam(_ context.Context, ws string, t Team) (*Team, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, false, err
	}
	t.Members = normalizeEmails(t.Members)
	t.Groups = dedupe(t.Groups)
	t.Everyone = false
	now := m.now()
	for i := range m.teams {
		e := &m.teams[i]
		if e.WorkspaceID == w.ID && e.Name == t.Name {
			if e.Everyone {
				return nil, false, ErrBuiltIn
			}
			e.Description, e.Members, e.Groups, e.PlatformRole, e.UpdatedAt = t.Description, t.Members, t.Groups, t.PlatformRole, now
			c := cloneTeam(*e)
			return &c, false, nil
		}
	}
	t.ID, t.WorkspaceID, t.CreatedAt, t.UpdatedAt = newID(), w.ID, now, now
	m.teams = append(m.teams, t)
	c := cloneTeam(t)
	return &c, true, nil
}

func (m *Memory) DeleteTeam(_ context.Context, ws, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return err
	}
	for i, t := range m.teams {
		if t.WorkspaceID == w.ID && t.Name == name {
			if t.Everyone {
				return ErrBuiltIn
			}
			m.teams = append(m.teams[:i], m.teams[i+1:]...)
			kept := m.grants[:0]
			for _, g := range m.grants {
				if !(g.WorkspaceID == w.ID && g.Team == name) {
					kept = append(kept, g)
				}
			}
			m.grants = kept
			// Invitations into the team stay, without the team (SET NULL).
			for j := range m.invitations {
				if e := &m.invitations[j]; e.inv.WorkspaceID == w.ID && e.inv.Team == name {
					e.inv.Team = ""
				}
			}
			return nil
		}
	}
	return ErrNotFound
}

func (m *Memory) ListGrants(_ context.Context, ws string) ([]Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	var out []Grant
	for _, g := range m.grants {
		if g.WorkspaceID == w.ID {
			out = append(out, g)
		}
	}
	sortGrants(out)
	return out, nil
}

func (m *Memory) ListProjectGrants(ctx context.Context, ws, project string) ([]Grant, error) {
	all, err := m.ListGrants(ctx, ws)
	if err != nil {
		return nil, err
	}
	var out []Grant
	for _, g := range all {
		if g.Project == project {
			out = append(out, g)
		}
	}
	return out, nil
}

func (m *Memory) AddGrant(_ context.Context, ws string, g Grant) (*Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	g.User = strings.ToLower(strings.TrimSpace(g.User))
	if g.Team != "" {
		found := false
		for _, t := range m.teams {
			if t.WorkspaceID == w.ID && t.Name == g.Team {
				found = true
			}
		}
		if !found {
			return nil, ErrNotFound
		}
	}
	for _, e := range m.grants {
		if e.WorkspaceID == w.ID && e.Project == g.Project && e.Role == g.Role && e.User == g.User && e.Team == g.Team {
			return nil, ErrConflict
		}
	}
	g.ID, g.WorkspaceID, g.CreatedAt = newID(), w.ID, m.now()
	m.grants = append(m.grants, g)
	c := g
	return &c, nil
}

func (m *Memory) DeleteGrant(_ context.Context, ws, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return err
	}
	for i, g := range m.grants {
		if g.WorkspaceID == w.ID && g.ID == id {
			m.grants = append(m.grants[:i], m.grants[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func (m *Memory) DeleteProjectGrants(_ context.Context, ws, project string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return err
	}
	kept := m.grants[:0]
	for _, g := range m.grants {
		if !(g.WorkspaceID == w.ID && g.Project == project) {
			kept = append(kept, g)
		}
	}
	m.grants = kept
	return nil
}

func (m *Memory) Export(ctx context.Context, ws string) (*Dump, error) {
	w, err := m.Workspace(ctx, ws)
	if err != nil {
		return nil, err
	}
	ids, _ := m.ListIdentities(ctx, ws)
	teams, _ := m.ListTeams(ctx, ws)
	grants, _ := m.ListGrants(ctx, ws)
	domains, _ := m.ListDomainClaims(ctx, ws)
	memberships, _ := m.ListMemberships(ctx, ws)
	hosts, _ := m.ListWorkspaceHosts(ctx, ws)
	return &Dump{Version: DumpVersion, Workspace: *w, Identities: ids, Teams: teams, Grants: grants, Domains: domains, Memberships: memberships, Hosts: hosts}, nil
}

func (m *Memory) Import(ctx context.Context, ws string, d *Dump, overwrite bool) (*ImportResult, error) {
	return importDump(ctx, m, ws, d, overwrite)
}

// importDump is the Import every implementation shares: it goes through the
// public methods so the rules (normalisation, team existence, conflicts)
// are the same everywhere.
func importDump(ctx context.Context, s Store, ws string, d *Dump, overwrite bool) (*ImportResult, error) {
	res := &ImportResult{}
	if d.Workspace.Name != "" && overwrite {
		if _, err := s.UpdateWorkspace(ctx, ws, d.Workspace.Name); err != nil {
			return res, err
		}
		if _, err := s.UpdateWorkspaceSettings(ctx, ws, d.Workspace.Settings); err != nil {
			return res, err
		}
	}
	for _, dc := range d.Domains {
		claim, err := s.PutDomainClaim(ctx, ws, dc.Domain, dc.Connector)
		if err != nil {
			return res, err
		}
		if dc.VerifiedAt != nil && claim.VerifiedAt == nil {
			_, _ = s.MarkDomainVerified(ctx, ws, dc.Domain, *dc.VerifiedAt)
		}
	}
	for _, t := range d.Teams {
		if t.Everyone || t.Name == TeamEveryone {
			continue // built in everywhere
		}
		if !overwrite {
			if _, err := s.GetTeam(ctx, ws, t.Name); err == nil {
				res.Skipped++
				continue
			}
		}
		if _, _, err := s.PutTeam(ctx, ws, t); err != nil {
			return res, err
		}
		res.Teams++
	}
	for _, g := range d.Grants {
		_, err := s.AddGrant(ctx, ws, g)
		switch {
		case err == nil:
			res.Grants++
		case err == ErrConflict:
			res.Skipped++
		default:
			return res, err
		}
	}
	for _, id := range d.Identities {
		if _, err := s.TouchIdentity(ctx, ws, id); err != nil {
			return res, err
		}
		if id.Status == StatusSuspended {
			_, _ = s.SetIdentityStatus(ctx, ws, id.Email, StatusSuspended)
		}
		res.Identities++
	}
	// Roles: a person's role is replaced only when overwriting; a role
	// unknown to this version is skipped rather than refused.
	existing := map[string]bool{}
	if !overwrite {
		have, err := s.ListMemberships(ctx, ws)
		if err != nil {
			return res, err
		}
		for _, mb := range have {
			existing[mb.Email] = true
		}
	}
	for _, mb := range d.Memberships {
		email := strings.ToLower(strings.TrimSpace(mb.Email))
		if !ValidWorkspaceRole(mb.Role) || existing[email] {
			res.Skipped++
			continue
		}
		if _, err := s.PutMembership(ctx, ws, email, mb.Role); err != nil {
			return res, err
		}
		res.Memberships++
	}
	// Hosts: custom domains come back verified as they were; a host taken
	// by another workspace on this platform is skipped.
	for _, h := range d.Hosts {
		if h.Kind != HostCustom {
			continue // moved addresses are not carried over
		}
		put, err := s.PutWorkspaceHost(ctx, ws, WorkspaceHost{Host: h.Host, Kind: h.Kind, Primary: h.Primary, VerifiedAt: h.VerifiedAt})
		switch {
		case err == nil:
			res.Hosts++
			_ = put
		case errors.Is(err, ErrConflict):
			res.Skipped++
		default:
			return res, err
		}
	}
	return res, nil
}

func cloneTeam(t Team) Team {
	t.Members = append([]string(nil), t.Members...)
	t.Groups = append([]string(nil), t.Groups...)
	return t
}

func sortGrants(gs []Grant) {
	sort.Slice(gs, func(i, j int) bool {
		if gs[i].Project != gs[j].Project {
			return gs[i].Project < gs[j].Project
		}
		if gs[i].Role != gs[j].Role {
			return gs[i].Role < gs[j].Role
		}
		return gs[i].User+gs[i].Team < gs[j].User+gs[j].Team
	})
}

// normalizeEmails lower-cases, trims and dedupes, keeping order.
func normalizeEmails(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, e := range in {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func (m *Memory) PutSession(_ context.Context, ws string, sess Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return err
	}
	sess.WorkspaceID = w.ID
	c := sess
	m.sessions[sess.ID] = &c
	return nil
}

func (m *Memory) GetSession(_ context.Context, id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	c := *s
	return &c, nil
}

func (m *Memory) TouchSession(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return ErrNotFound
	}
	s.LastSeenAt = at
	return nil
}

func (m *Memory) DeleteSession(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
	return nil
}

func (m *Memory) PurgeSessions(_ context.Context, createdBefore, seenBefore time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id, s := range m.sessions {
		if s.CreatedAt.Before(createdBefore) || s.LastSeenAt.Before(seenBefore) {
			delete(m.sessions, id)
			n++
		}
	}
	return n, nil
}

func (m *Memory) CountSessions(_ context.Context, ws string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, s := range m.sessions {
		if s.WorkspaceID == w.ID {
			n++
		}
	}
	return n, nil
}

func (m *Memory) PutCode(_ context.Context, c Code) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for k, e := range m.codes {
		if now.After(e.ExpiresAt) {
			delete(m.codes, k)
		}
	}
	cc := c
	m.codes[c.Code] = &cc
	return nil
}

func (m *Memory) TakeCode(_ context.Context, code string) (*Code, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.codes[code]
	if !ok {
		return nil, ErrNotFound
	}
	delete(m.codes, code)
	if m.now().After(c.ExpiresAt) {
		return nil, ErrNotFound
	}
	cc := *c
	return &cc, nil
}

// ---- API tokens (RFC-0031) -------------------------------------------------

func (m *Memory) CreateToken(_ context.Context, ws string, t APIToken, hash string) (*APIToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	t.OwnerEmail = strings.ToLower(t.OwnerEmail)
	for _, e := range m.tokens {
		if e.token.WorkspaceID == w.ID && e.token.OwnerEmail == t.OwnerEmail && e.token.Name == t.Name {
			return nil, ErrConflict
		}
	}
	t.ID, t.WorkspaceID = newID(), w.ID
	t.CreatedAt = m.now()
	c := t
	m.tokens = append(m.tokens, tokenEntry{token: c, hash: hash})
	return &c, nil
}

func (m *Memory) LookupToken(_ context.Context, hash string) (*APIToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for i := range m.tokens {
		e := &m.tokens[i]
		if e.hash != hash {
			continue
		}
		if e.token.ExpiresAt != nil && now.After(*e.token.ExpiresAt) {
			return nil, nil
		}
		if now.Sub(e.lastUpdated) > time.Minute {
			e.token.LastUsedAt = &now
			e.lastUpdated = now
		}
		c := e.token
		return &c, nil
	}
	return nil, nil
}

func (m *Memory) ListTokens(_ context.Context, ws, ownerEmail string) ([]APIToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	var out []APIToken
	for _, e := range m.tokens {
		if e.token.WorkspaceID != w.ID {
			continue
		}
		if ownerEmail != "" && !strings.EqualFold(e.token.OwnerEmail, ownerEmail) {
			continue
		}
		out = append(out, e.token)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *Memory) DeleteToken(_ context.Context, ws, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return err
	}
	for i, e := range m.tokens {
		if e.token.WorkspaceID == w.ID && e.token.ID == id {
			m.tokens = append(m.tokens[:i], m.tokens[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

// ---- workspace roles and invitations (RFC-0033) ------------------------------

func (m *Memory) ListMemberships(_ context.Context, ws string) ([]Membership, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	var out []Membership
	for _, mb := range m.memberships {
		if mb.WorkspaceID == w.ID {
			out = append(out, mb)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

func (m *Memory) PutMembership(_ context.Context, ws, email, role string) (*Membership, error) {
	if !ValidWorkspaceRole(role) {
		return nil, fmt.Errorf("role %q is not a workspace role", role)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, errors.New("email is required")
	}
	now := m.now()
	for i := range m.memberships {
		mb := &m.memberships[i]
		if mb.WorkspaceID == w.ID && mb.Email == email {
			if mb.Role != role {
				mb.Role, mb.UpdatedAt = role, now
			}
			c := *mb
			return &c, nil
		}
	}
	mb := Membership{ID: newID(), WorkspaceID: w.ID, Email: email, Role: role, CreatedAt: now, UpdatedAt: now}
	m.memberships = append(m.memberships, mb)
	return &mb, nil
}

func (m *Memory) DeleteMembership(_ context.Context, ws, email string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	for i, mb := range m.memberships {
		if mb.WorkspaceID == w.ID && mb.Email == email {
			m.memberships = append(m.memberships[:i], m.memberships[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func (m *Memory) ListInvitations(_ context.Context, ws string) ([]Invitation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	var out []Invitation
	for _, e := range m.invitations {
		if e.inv.WorkspaceID == w.ID {
			out = append(out, e.inv)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].Email < out[j].Email
	})
	return out, nil
}

func (m *Memory) CreateInvitation(_ context.Context, ws string, inv Invitation, tokenHash string) (*Invitation, error) {
	if !ValidWorkspaceRole(inv.Role) {
		return nil, fmt.Errorf("role %q is not a workspace role", inv.Role)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	inv.Email = strings.ToLower(strings.TrimSpace(inv.Email))
	if inv.Email == "" || tokenHash == "" {
		return nil, errors.New("email and token are required")
	}
	if inv.Team != "" {
		found := false
		for _, t := range m.teams {
			if t.WorkspaceID == w.ID && t.Name == inv.Team {
				found = true
			}
		}
		if !found {
			return nil, ErrNotFound
		}
	}
	kept := m.invitations[:0]
	for _, e := range m.invitations {
		if !(e.inv.WorkspaceID == w.ID && e.inv.Email == inv.Email) {
			kept = append(kept, e)
		}
	}
	m.invitations = kept
	now := m.now()
	inv.ID, inv.WorkspaceID, inv.CreatedAt = newID(), w.ID, now
	if inv.ExpiresAt.IsZero() {
		inv.ExpiresAt = now.Add(7 * 24 * time.Hour)
	}
	m.invitations = append(m.invitations, invitationEntry{inv: inv, hash: tokenHash})
	c := inv
	return &c, nil
}

func (m *Memory) InvitationByToken(_ context.Context, tokenHash string) (*Invitation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.invitations {
		if tokenHash != "" && e.hash == tokenHash {
			c := e.inv
			return &c, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) DeleteInvitation(_ context.Context, ws, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return err
	}
	for i, e := range m.invitations {
		if e.inv.WorkspaceID == w.ID && e.inv.ID == id {
			m.invitations = append(m.invitations[:i], m.invitations[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

// ---- workspace hosts (RFC-0033 names) -------------------------------------------

func normalizeHost(h string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), "."))
}

func (m *Memory) ListWorkspaceHosts(_ context.Context, ws string) ([]WorkspaceHost, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	var out []WorkspaceHost
	for _, h := range m.hosts {
		if h.WorkspaceID == w.ID {
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out, nil
}

// hostTaken says a host equals some workspace's address or a host record
// of a workspace other than exceptID.
func (m *Memory) hostTaken(host, exceptWorkspaceID string) bool {
	for _, w := range m.workspaces {
		if w.Address != "" && w.Address == host && w.ID != exceptWorkspaceID {
			return true
		}
	}
	for _, h := range m.hosts {
		if h.Host == host && h.WorkspaceID != exceptWorkspaceID {
			return true
		}
	}
	return false
}

func (m *Memory) PutWorkspaceHost(_ context.Context, ws string, h WorkspaceHost) (*WorkspaceHost, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	h.Host = normalizeHost(h.Host)
	if h.Host == "" {
		return nil, errors.New("host is required")
	}
	if h.Kind != HostCustom && h.Kind != HostMoved {
		return nil, fmt.Errorf("host kind %q is not custom or moved", h.Kind)
	}
	if m.hostTaken(h.Host, w.ID) || (w.Address != "" && w.Address == h.Host) {
		return nil, ErrConflict
	}
	if h.Primary {
		for i := range m.hosts {
			if m.hosts[i].WorkspaceID == w.ID {
				m.hosts[i].Primary = false
			}
		}
	}
	for i := range m.hosts {
		e := &m.hosts[i]
		if e.WorkspaceID == w.ID && e.Host == h.Host {
			e.Primary, e.VerifiedAt, e.ExpiresAt = h.Primary, h.VerifiedAt, h.ExpiresAt
			c := *e
			return &c, nil
		}
	}
	h.ID, h.WorkspaceID, h.CreatedAt = newID(), w.ID, m.now()
	if h.Kind == HostCustom && h.Token == "" {
		h.Token = newID()
	}
	m.hosts = append(m.hosts, h)
	c := h
	return &c, nil
}

func (m *Memory) DeleteWorkspaceHost(_ context.Context, ws, host string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return err
	}
	host = normalizeHost(host)
	for i, h := range m.hosts {
		if h.WorkspaceID == w.ID && h.Host == host {
			m.hosts = append(m.hosts[:i], m.hosts[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func (m *Memory) WorkspaceByHost(_ context.Context, host string) (*Workspace, *WorkspaceHost, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	host = normalizeHost(host)
	for _, h := range m.hosts {
		if h.Host == host {
			for _, w := range m.workspaces {
				if w.ID == h.WorkspaceID {
					cw, ch := *w, h
					return &cw, &ch, nil
				}
			}
		}
	}
	return nil, nil, ErrNotFound
}

func (m *Memory) UpdateWorkspaceAddress(_ context.Context, slug, address string) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(slug)
	if err != nil {
		return nil, err
	}
	address = normalizeHost(address)
	if address == "" {
		return nil, errors.New("address is required")
	}
	if m.hostTaken(address, w.ID) {
		return nil, ErrConflict
	}
	for _, h := range m.hosts {
		if h.WorkspaceID == w.ID && h.Host == address {
			return nil, ErrConflict // one of its own hosts
		}
	}
	w.Address, w.UpdatedAt = address, m.now()
	c := *w
	return &c, nil
}

// ---- OAuth 2.1 server (RFC-0032) ----------------------------------------------

func (m *Memory) CreateOAuthClient(_ context.Context, ws string, c OAuthClient) (*OAuthClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	if c.ClientID == "" {
		return nil, errors.New("client id is required")
	}
	for _, e := range m.oclients {
		if e.ClientID == c.ClientID {
			return nil, ErrConflict
		}
	}
	c.ID, c.WorkspaceID, c.CreatedAt = newID(), w.ID, m.now()
	c.RedirectURIs = append([]string(nil), c.RedirectURIs...)
	m.oclients = append(m.oclients, c)
	out := c
	return &out, nil
}

func (m *Memory) OAuthClientByID(_ context.Context, clientID string) (*OAuthClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.oclients {
		if e.ClientID == clientID {
			out := e
			return &out, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) PutOAuthCode(_ context.Context, code OAuthCode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if code.Hash == "" {
		return errors.New("code hash is required")
	}
	if m.ocodes == nil {
		m.ocodes = map[string]*OAuthCode{}
	}
	now := m.now()
	for k, c := range m.ocodes {
		if now.After(c.ExpiresAt) {
			delete(m.ocodes, k)
		}
	}
	c := code
	c.Email = strings.ToLower(c.Email)
	m.ocodes[code.Hash] = &c
	return nil
}

func (m *Memory) TakeOAuthCode(_ context.Context, hash string) (*OAuthCode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.ocodes[hash]
	if !ok {
		return nil, ErrNotFound
	}
	delete(m.ocodes, hash)
	if m.now().After(c.ExpiresAt) {
		return nil, ErrNotFound
	}
	out := *c
	return &out, nil
}

func (m *Memory) CreateOAuthToken(_ context.Context, ws string, t OAuthToken) (*OAuthToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	if t.Hash == "" {
		return nil, errors.New("token hash is required")
	}
	t.ID, t.WorkspaceID, t.CreatedAt = newID(), w.ID, m.now()
	t.Email = strings.ToLower(t.Email)
	m.otokens = append(m.otokens, t)
	out := t
	return &out, nil
}

func (m *Memory) OAuthTokenByHash(_ context.Context, hash string) (*OAuthToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.otokens {
		if t.Hash == hash {
			if m.now().After(t.ExpiresAt) {
				return nil, ErrNotFound
			}
			out := t
			for _, c := range m.oclients {
				if c.ClientID == t.ClientID {
					out.ClientName = c.Name
				}
			}
			return &out, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) RotateOAuthToken(_ context.Context, id, newHash string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.otokens {
		if m.otokens[i].ID == id {
			now := m.now()
			m.otokens[i].Hash, m.otokens[i].ExpiresAt, m.otokens[i].LastUsedAt = newHash, expiresAt, &now
			return nil
		}
	}
	return ErrNotFound
}

func (m *Memory) ListOAuthTokens(_ context.Context, ws, email string) ([]OAuthToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, c := range m.oclients {
		names[c.ClientID] = c.Name
	}
	var out []OAuthToken
	for _, t := range m.otokens {
		if t.WorkspaceID == w.ID && (email == "" || strings.EqualFold(t.Email, email)) {
			t.ClientName = names[t.ClientID]
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) DeleteOAuthToken(_ context.Context, ws, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return err
	}
	for i, t := range m.otokens {
		if t.WorkspaceID == w.ID && t.ID == id {
			m.otokens = append(m.otokens[:i], m.otokens[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

// ---- billing (RFC-0075) -------------------------------------------------------

type billingMemory struct {
	plans       []Plan
	wplans      []WorkspacePlan
	buckets     []UsageBucket
	hourly      []UsageBucket
	invoices    []InvoiceLine
	cogs        []COGSBucket
	sleepEvents []SleepEvent
}

func (m *Memory) billing() *billingMemory {
	if m.bill == nil {
		m.bill = &billingMemory{}
	}
	return m.bill
}

func (m *Memory) CreatePlan(_ context.Context, p Plan) (*Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.billing().plans {
		if e.Name == p.Name {
			return nil, ErrConflict
		}
	}
	p.ID, p.CreatedAt = newID(), m.now()
	if p.Currency == "" {
		p.Currency = "USD"
	}
	m.billing().plans = append(m.billing().plans, p)
	out := p
	return &out, nil
}
func (m *Memory) ListPlans(_ context.Context) ([]Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Plan(nil), m.billing().plans...), nil
}
func (m *Memory) GetPlan(_ context.Context, nameOrID string) (*Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.billing().plans {
		if p.ID == nameOrID || p.Name == nameOrID {
			out := p
			return &out, nil
		}
	}
	return nil, ErrNotFound
}
func (m *Memory) AssignPlan(ctx context.Context, ws, nameOrID string) (*WorkspacePlan, error) {
	p, err := m.GetPlan(ctx, nameOrID)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	now := m.now()
	for i := range m.billing().wplans {
		if m.billing().wplans[i].WorkspaceID == w.ID && m.billing().wplans[i].EndsAt == nil {
			m.billing().wplans[i].EndsAt = &now
		}
	}
	wp := WorkspacePlan{ID: newID(), WorkspaceID: w.ID, PlanID: p.ID, PlanName: p.Name, StartsAt: now}
	m.billing().wplans = append(m.billing().wplans, wp)
	out := wp
	return &out, nil
}
func (m *Memory) WorkspacePlan(ctx context.Context, ws string) (*WorkspacePlan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	for i := range m.billing().wplans {
		wp := &m.billing().wplans[i]
		if wp.WorkspaceID == w.ID && wp.EndsAt == nil {
			out := *wp
			return &out, nil
		}
	}
	return nil, ErrNotFound
}
func (m *Memory) WorkspacePlanHistory(ctx context.Context, ws string) ([]WorkspacePlan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.ws(ws)
	if err != nil {
		return nil, err
	}
	var out []WorkspacePlan
	for _, wp := range m.billing().wplans {
		if wp.WorkspaceID == w.ID {
			out = append(out, wp)
		}
	}
	return out, nil
}

func (m *Memory) WriteBuckets(_ context.Context, buckets []UsageBucket) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range buckets {
		// Accept a slug or an id, like the Postgres store.
		if w, err := m.ws(b.WorkspaceID); err == nil {
			b.WorkspaceID = w.ID
		} else if !m.hasWorkspaceID(b.WorkspaceID) {
			continue // unknown workspace: skip
		}
		dup := false
		for _, e := range m.billing().buckets {
			if e.WorkspaceID == b.WorkspaceID && e.Project == b.Project && e.Component == b.Component &&
				e.Metric == b.Metric && e.PeriodStart.Equal(b.PeriodStart) && e.Revision == b.Revision {
				dup = true
				break
			}
		}
		if !dup {
			m.billing().buckets = append(m.billing().buckets, b)
		}
	}
	return nil
}

// RollupHourly mirrors the Postgres rollup on the in-memory tables.
func (m *Memory) RollupHourly(_ context.Context, before time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := before.UTC().Truncate(time.Hour)
	type key struct {
		ws, project, component, metric string
		hour                           time.Time
	}
	type agg struct {
		sum      float64
		nilQty   bool
		missing  bool
		partial  bool
		n        int
		unit     string
		revision int
		source   string
	}
	groups := map[key]*agg{}
	var keep []UsageBucket
	for _, b := range m.billing().buckets {
		if !b.PeriodStart.Before(cutoff) {
			keep = append(keep, b)
			continue
		}
		k := key{b.WorkspaceID, b.Project, b.Component, b.Metric, b.PeriodStart.UTC().Truncate(time.Hour)}
		a := groups[k]
		if a == nil {
			a = &agg{unit: b.Unit, source: b.Source}
			groups[k] = a
		}
		a.n++
		if b.Quantity == nil {
			a.nilQty = true
		} else {
			a.sum += *b.Quantity
		}
		a.missing = a.missing || b.Quality == QualityMissing
		a.partial = a.partial || b.Quality == QualityPartial
		if b.Revision > a.revision {
			a.revision = b.Revision
		}
	}
	existing := map[key]bool{}
	for _, h := range m.billing().hourly {
		existing[key{h.WorkspaceID, h.Project, h.Component, h.Metric, h.PeriodStart}] = true
	}
	rolled := 0
	for k, a := range groups {
		if existing[k] {
			continue
		}
		q := QualityComplete
		switch {
		case a.missing:
			q = QualityMissing
		case a.partial || a.n < 12:
			q = QualityPartial
		}
		var qty *float64
		if !a.nilQty {
			v := a.sum
			qty = &v
		}
		m.billing().hourly = append(m.billing().hourly, UsageBucket{
			WorkspaceID: k.ws, Project: k.project, Component: k.component, Metric: k.metric,
			PeriodStart: k.hour, PeriodEnd: k.hour.Add(time.Hour),
			Quantity: qty, Unit: a.unit, Quality: q, Revision: a.revision, Source: a.source,
		})
		rolled++
	}
	m.billing().buckets = keep
	return rolled, nil
}

func (m *Memory) QueryBuckets(_ context.Context, ws, project string, from, to time.Time) ([]UsageBucket, error) {
	m.mu.Lock()
	w, err := m.ws(ws)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []UsageBucket
	for _, b := range append(append([]UsageBucket(nil), m.billing().buckets...), m.billing().hourly...) {
		if b.WorkspaceID != w.ID {
			continue
		}
		if project != "" && b.Project != project {
			continue
		}
		if !b.PeriodStart.Before(to) || !b.PeriodEnd.After(from) {
			continue
		}
		out = append(out, b)
	}
	return out, nil
}
func (m *Memory) UpsertInvoiceLine(_ context.Context, line InvoiceLine) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if line.ID == "" {
		line.ID = newID()
	}
	if line.CreatedAt.IsZero() {
		line.CreatedAt = m.now()
	}
	for i, e := range m.billing().invoices {
		if e.WorkspaceID == line.WorkspaceID && e.PeriodStart.Equal(line.PeriodStart) &&
			e.Component == line.Component && e.Metric == line.Metric && e.Revision == line.Revision {
			m.billing().invoices[i] = line
			return nil
		}
	}
	m.billing().invoices = append(m.billing().invoices, line)
	return nil
}
func (m *Memory) QueryInvoiceLines(_ context.Context, ws string, from, to time.Time, finalized *bool) ([]InvoiceLine, error) {
	m.mu.Lock()
	w, err := m.ws(ws)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []InvoiceLine
	for _, l := range m.billing().invoices {
		if l.WorkspaceID != w.ID {
			continue
		}
		if !l.PeriodStart.Before(to) || !l.PeriodEnd.After(from) {
			continue
		}
		if finalized != nil && l.Finalized != *finalized {
			continue
		}
		out = append(out, l)
	}
	return out, nil
}
func (m *Memory) WriteCOGSBucket(_ context.Context, b COGSBucket) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, e := range m.billing().cogs {
		if e.WorkspaceID == b.WorkspaceID && e.Project == b.Project && e.PeriodStart.Equal(b.PeriodStart) {
			m.billing().cogs[i] = b
			return nil
		}
	}
	m.billing().cogs = append(m.billing().cogs, b)
	return nil
}
func (m *Memory) QueryCOGSBuckets(_ context.Context, ws string, from, to time.Time) ([]COGSBucket, error) {
	m.mu.Lock()
	w, err := m.ws(ws)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []COGSBucket
	for _, b := range m.billing().cogs {
		if b.WorkspaceID == w.ID && b.PeriodStart.Before(to) && b.PeriodEnd.After(from) {
			out = append(out, b)
		}
	}
	return out, nil
}
func (m *Memory) WriteSleepEvent(_ context.Context, e SleepEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.ID == "" {
		e.ID = newID()
	}
	if e.At.IsZero() {
		e.At = m.now()
	}
	m.billing().sleepEvents = append(m.billing().sleepEvents, e)
	return nil
}
func (m *Memory) QuerySleepEvents(_ context.Context, ws, project string, from, to time.Time) ([]SleepEvent, error) {
	m.mu.Lock()
	w, err := m.ws(ws)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []SleepEvent
	for _, e := range m.billing().sleepEvents {
		if e.WorkspaceID != w.ID {
			continue
		}
		if project != "" && e.Project != project {
			continue
		}
		if !e.At.Before(to) || e.At.Before(from) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}
