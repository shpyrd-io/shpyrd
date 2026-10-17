// Package githubtest is a fake GitHub for shpyrd's tests: the App's token
// endpoint (which checks the App's JWT), an installation's repositories and
// branches, people with GitHub App user tokens (exchanged, refreshed, revoked) and
// what each may read, a person's installations and the manifest
// conversion, a repository's compare, check runs, statuses and Deployments.
// Its state is read and set through methods, under its lock.
package githubtest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/shpyrd-io/shpyrd/pkg/github"
)

// The App the fake knows, and the codes it accepts.
const (
	AppID         = 1234
	Slug          = "shpyrd-test"
	WebhookSecret = "whsec-test"
	ClientID      = "Iv1.test"
	ClientSecret  = "cs-test"
	Code          = "good-code"
	UserToken     = "gho_person"
	// UserLogin is who UserToken is, an owner of Acme until SetOrgRole.
	UserLogin      = "ana"
	ConversionCode = "c0de"
)

var (
	keyOnce sync.Once
	key     *rsa.PrivateKey
)

// Key is the App's private key: one per test binary.
func Key() *rsa.PrivateKey {
	keyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		key = k
	})
	return key
}

// KeyPEM is Key as GitHub hands it over.
func KeyPEM() string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(Key())}))
}

// Repo is a repository an installation grants.
type Repo struct {
	FullName      string
	Private       bool
	DefaultBranch string
}

// Installation is one a person can see.
type Installation struct {
	ID          int64
	Login, Type string
	Suspended   bool
}

// Server is the fake.
type Server struct {
	*httptest.Server

	mu   sync.Mutex
	now  time.Time
	down bool
	// limited is the Retry-After of a rate limit; 0: none.
	limited time.Duration
	// forbidden answers 403, not for a rate limit.
	forbidden  bool
	repos      map[int64][]Repo
	branches   map[string][]string // by full name, lower case
	people     []Installation
	users      map[string]*User     // by handle: the login in lower case, "default" for the default person
	codes      map[string]string    // OAuth code -> handle
	access     map[string]userGrant // user access token -> whose, until when
	refresh    map[string]string    // live refresh token -> handle
	revoked    []string
	issued     int
	noExpiring bool
	public     []Repo
	orgRoles   map[string][2]string // by org, lower case: role, state
	// membersRefusal is the message of a 403 the memberships call answers;
	// "": none.
	membersRefusal string
	minted         map[int64]int
	tokens         map[string]tokenGrant // installation token -> what it may do
	lastBody       map[string]any
	calls          []string
	compares       map[string]Compare
	runs           map[string][]CheckRun // by commit, lower case
	statuses       map[string][]Status   // by commit, lower case
	deployments    []Deployment
	nextID         int64
	writes         []Write
	totals         map[string]int  // reported total_count by kind and commit
	refused        map[string]bool // permissions a token request is refused for
	compareQuery   string
}

// tokenGrant is an installation token: its installation, the repositories
// it was asked for (none: all of the installation's) and its permissions.
type tokenGrant struct {
	installation int64
	repos        []string
	perms        map[string]string
}

// NewServer starts the fake with installation 42 on Acme.
func NewServer(t testing.TB) *Server {
	f := &Server{
		repos: map[int64][]Repo{42: {
			{FullName: "Acme/shop", Private: true, DefaultBranch: "main"},
			{FullName: "Acme/site", DefaultBranch: "trunk"},
		}},
		branches: map[string][]string{"acme/shop": {"dev", "main"}, "acme/site": {"trunk"}},
		people:   []Installation{{ID: 42, Login: "Acme", Type: "Organization"}},
		users:    map[string]*User{defaultUser: {ID: UserID, Login: UserLogin}},
		codes:    map[string]string{Code: defaultUser},
		access:   map[string]userGrant{UserToken: {handle: defaultUser, exp: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)}},
		refresh:  map[string]string{},
		orgRoles: map[string][2]string{"acme": {"admin", "active"}},
		minted:   map[int64]int{},
		tokens:   map[string]tokenGrant{},
		compares: map[string]Compare{},
		runs:     map[string][]CheckRun{},
		statuses: map[string][]Status{},
		totals:   map[string]int{},
		refused:  map[string]bool{},
	}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

// App is the App's credentials, its html-url on the fake.
func (f *Server) App() github.App {
	return github.App{ID: AppID, Slug: Slug, PrivateKey: KeyPEM(), WebhookSecret: WebhookSecret,
		ClientID: ClientID, ClientSecret: ClientSecret, HTMLURL: f.URL + "/apps/" + Slug}
}

// Secret is the App's Secret in namespace.
func (f *Server) Secret(namespace string) *corev1.Secret {
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: github.SecretName, Namespace: namespace},
		Type: corev1.SecretTypeOpaque, Data: f.App().SecretData()}
}

// Options reach the fake, on its clock.
func (f *Server) Options() github.Options {
	return github.Options{APIURL: f.URL, HTTP: f.Client(), Now: f.Time}
}

// Source reads the App's Secret through r.
func (f *Server) Source(r client.Reader, namespace string) *github.Source {
	return &github.Source{Reader: r, Namespace: namespace, Options: f.Options()}
}

// Time is the fake's clock: SetNow's time, or the real one.
func (f *Server) Time() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clock()
}

func (f *Server) clock() time.Time {
	if f.now.IsZero() {
		return time.Now()
	}
	return f.now
}

func (f *Server) SetNow(t time.Time) { f.mu.Lock(); f.now = t; f.mu.Unlock() }

// SetRateLimited makes every call answer 429 with Retry-After d; 0 lifts it.
func (f *Server) SetRateLimited(d time.Duration) { f.mu.Lock(); f.limited = d; f.mu.Unlock() }

// SetForbidden makes every call answer 403 as GitHub refusing the App,
// not limiting its rate.
func (f *Server) SetForbidden(forbidden bool) { f.mu.Lock(); f.forbidden = forbidden; f.mu.Unlock() }

// SetDown makes every call answer 503.
func (f *Server) SetDown(down bool) { f.mu.Lock(); f.down = down; f.mu.Unlock() }

func (f *Server) SetRepos(installation int64, repos ...Repo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repos[installation] = repos
}

func (f *Server) SetUserInstallations(list ...Installation) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.people = list
}

// SetUserLogin is who the person's token says they are.
func (f *Server) SetUserLogin(login string) {
	f.mu.Lock()
	f.users[defaultUser].Login = login
	f.mu.Unlock()
}

// SetOrgRole sets the person's membership of org; role "" is none.
func (f *Server) SetOrgRole(org, role, state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if role == "" {
		delete(f.orgRoles, strings.ToLower(org))
		return
	}
	f.orgRoles[strings.ToLower(org)] = [2]string{role, state}
}

// SetNoMembersPermission makes the memberships call answer 403, as for an
// App whose owner has not accepted members: read.
func (f *Server) SetNoMembersPermission(no bool) {
	msg := ""
	if no {
		msg = "Resource not accessible by integration"
	}
	f.SetMembershipRefusal(msg)
}

// SetMembershipRefusal makes the memberships call answer 403 with message:
// an organisation's SAML enforcement, or a spent rate limit, say so in
// it. "" lifts it.
func (f *Server) SetMembershipRefusal(message string) {
	f.mu.Lock()
	f.membersRefusal = message
	f.mu.Unlock()
}

// Uninstall forgets an installation, as GitHub does once the App is
// uninstalled: its token calls answer 404.
func (f *Server) Uninstall(installation int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.repos, installation)
}

// MintedFor counts the tokens minted for an installation.
func (f *Server) MintedFor(installation int64) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.minted[installation]
}

// LastTokenRequest is the body of the last token request.
func (f *Server) LastTokenRequest() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastBody
}

// Calls are "METHOD /path" of every request, in order.
func (f *Server) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *Server) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	if f.limited > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(f.limited/time.Second)))
		reply(w, http.StatusTooManyRequests, map[string]string{"message": "API rate limit exceeded"})
		return
	}
	if f.forbidden {
		reply(w, http.StatusForbidden, map[string]string{"message": "Resource not accessible by integration"})
		return
	}
	if f.down {
		reply(w, http.StatusServiceUnavailable, map[string]string{"message": "Service unavailable"})
		return
	}
	p := r.URL.Path
	switch {
	case r.Method == "POST" && strings.HasPrefix(p, "/app/installations/") && strings.HasSuffix(p, "/access_tokens"):
		f.accessToken(w, r, strings.TrimSuffix(strings.TrimPrefix(p, "/app/installations/"), "/access_tokens"))
	case r.Method == "GET" && p == "/installation/repositories":
		f.repositories(w, r)
	case r.Method == "GET" && strings.HasPrefix(p, "/repos/") && strings.Count(p, "/") == 3:
		f.repoInfo(w, r, strings.TrimPrefix(p, "/repos/"))
	case r.Method == "GET" && strings.HasPrefix(p, "/user/installations/") && strings.HasSuffix(p, "/repositories"):
		f.userInstallationRepos(w, r, strings.TrimSuffix(strings.TrimPrefix(p, "/user/installations/"), "/repositories"))
	case r.Method == "DELETE" && p == "/applications/"+ClientID+"/token":
		f.revoke(w, r)
	case r.Method == "GET" && strings.HasPrefix(p, "/repos/") && strings.HasSuffix(p, "/branches"):
		f.branchList(w, r, strings.TrimSuffix(strings.TrimPrefix(p, "/repos/"), "/branches"))
	case strings.HasPrefix(p, "/repos/"):
		f.repoCall(w, r, p)
	case r.Method == "POST" && p == "/login/oauth/access_token":
		f.exchange(w, r)
	case r.Method == "GET" && p == "/user/installations":
		f.userInstallations(w, r)
	case r.Method == "GET" && p == "/user":
		f.user(w, r)
	case r.Method == "GET" && strings.HasPrefix(p, "/user/memberships/orgs/"):
		f.membership(w, r, strings.TrimPrefix(p, "/user/memberships/orgs/"))
	case r.Method == "POST" && strings.HasPrefix(p, "/app-manifests/") && strings.HasSuffix(p, "/conversions"):
		f.conversion(w, strings.TrimSuffix(strings.TrimPrefix(p, "/app-manifests/"), "/conversions"))
	default:
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	}
}

func (f *Server) accessToken(w http.ResponseWriter, r *http.Request, id string) {
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), claims,
		func(*jwt.Token) (any, error) { return &Key().PublicKey, nil },
		jwt.WithValidMethods([]string{"RS256"}), jwt.WithTimeFunc(f.clock), jwt.WithIssuer(strconv.Itoa(AppID)))
	if err != nil {
		reply(w, http.StatusUnauthorized, map[string]string{"message": "A JSON web token could not be decoded"})
		return
	}
	installation, _ := strconv.ParseInt(id, 10, 64)
	repos, ok := f.repos[installation]
	if !ok {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var body struct {
		Repositories []string          `json:"repositories"`
		Permissions  map[string]string `json:"permissions"`
	}
	_ = json.Unmarshal(raw, &body)
	f.lastBody = map[string]any{}
	_ = json.Unmarshal(raw, &f.lastBody)
	for perm := range body.Permissions {
		if f.refused[perm] {
			reply(w, http.StatusUnprocessableEntity, map[string]string{"message": "The permissions requested are not granted to this installation."})
			return
		}
	}
	for _, name := range body.Repositories {
		if !grants(repos, name) {
			reply(w, http.StatusUnprocessableEntity, map[string]string{"message": "There is at least one repository that does not exist or is not accessible to the parent installation."})
			return
		}
	}
	f.minted[installation]++
	tok := fmt.Sprintf("ghs_%d_%d", installation, f.minted[installation])
	f.tokens[tok] = tokenGrant{installation: installation, repos: body.Repositories, perms: body.Permissions}
	reply(w, http.StatusCreated, map[string]any{"token": tok, "expires_at": f.clock().Add(time.Hour).UTC().Format(time.RFC3339)})
}

func grants(repos []Repo, name string) bool {
	for _, r := range repos {
		if _, n, _ := strings.Cut(r.FullName, "/"); strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

// installationOf is the installation an installation token is of.
func (f *Server) installationOf(r *http.Request) (int64, bool) {
	g, ok := f.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	return g.installation, ok
}

func (f *Server) repositories(w http.ResponseWriter, r *http.Request) {
	id, ok := f.installationOf(r)
	if !ok {
		reply(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	var out []map[string]any
	for _, repo := range pageOf(f.repos[id], r) {
		out = append(out, map[string]any{"full_name": repo.FullName, "private": repo.Private, "default_branch": repo.DefaultBranch})
	}
	reply(w, http.StatusOK, map[string]any{"total_count": len(f.repos[id]), "repositories": out})
}

func (f *Server) branchList(w http.ResponseWriter, r *http.Request, fullName string) {
	allowed := false
	if id, ok := f.installationOf(r); ok {
		allowed = grants(f.repos[id], fullName[strings.Index(fullName, "/")+1:])
	} else if _, ok := f.userOf(r); ok {
		_, allowed = f.readable(r, fullName)
	}
	if !allowed {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	out := []map[string]string{}
	for _, b := range pageOf(f.branches[strings.ToLower(fullName)], r) {
		out = append(out, map[string]string{"name": b})
	}
	reply(w, http.StatusOK, out)
}

func (f *Server) membership(w http.ResponseWriter, r *http.Request, org string) {
	if _, ok := f.userOf(r); !ok {
		reply(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	if f.membersRefusal != "" {
		reply(w, http.StatusForbidden, map[string]string{"message": f.membersRefusal})
		return
	}
	m, ok := f.orgRoles[strings.ToLower(org)]
	if !ok {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	reply(w, http.StatusOK, map[string]any{"role": m[0], "state": m[1], "organization": map[string]string{"login": org}})
}

func (f *Server) conversion(w http.ResponseWriter, code string) {
	if code != ConversionCode {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	app := f.App()
	reply(w, http.StatusCreated, map[string]any{
		"id": app.ID, "slug": app.Slug, "name": "shpyrd test", "owner": map[string]string{"login": "acme"},
		"html_url": app.HTMLURL, "client_id": app.ClientID, "client_secret": app.ClientSecret,
		"webhook_secret": app.WebhookSecret, "pem": app.PrivateKey,
	})
}

// pageOf is one page of items, as GitHub's per_page and page say.
func pageOf[T any](items []T, r *http.Request) []T {
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if per <= 0 {
		per = 30
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}
	from := min((page-1)*per, len(items))
	return items[from:min(from+per, len(items))]
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
