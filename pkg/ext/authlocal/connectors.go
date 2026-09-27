package authlocal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Dex connectors (RFC-0058): GitHub and Google sign in through Dex, which
// reads connectors from its Kubernetes storage as Connector objects, so
// adding one is an API call, not a Dex restart. The dashboard shows a
// button per connector and sends Dex straight to it (connector_id).

// ConnectorGVR is Dex's connector resource.
var ConnectorGVR = schema.GroupVersionResource{Group: "dex.coreos.com", Version: "v1", Resource: "connectors"}

// ConnectorKinds the CLI and the Workspace page know how to configure.
var ConnectorKinds = []string{"github", "google", "microsoft", "oidc"}

var connectorIDRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,30}[a-z0-9])?$`)

// Connector is a Dex connector as shown to people: no secrets.
type Connector struct {
	// ID is the connector's id within its scope: "google" for the
	// platform's Google method and for workspace acme's own. FullID is
	// Dex's connector id and the sign-in provider's id, unique on the
	// platform: "google", or "ws-acme-google".
	ID     string `json:"id"`
	FullID string `json:"-"`
	Type   string `json:"type"`
	Name   string `json:"name"`
	// Detail summarises the configuration (organisation, hosted domain).
	Detail string `json:"detail,omitempty"`
	// Workspace is the slug of the workspace that owns the method
	// (RFC-0033 per-workspace SSO); empty for the platform's.
	Workspace string `json:"workspace,omitempty"`
}

// LabelWorkspace marks a connector as a workspace's own.
const LabelWorkspace = "shpyrd.io/workspace"

// workspacePrefix starts the full id of a workspace's connector.
const workspacePrefix = "ws-"

// FullConnectorID is Dex's id for a connector: the platform's keep their
// id, a workspace's is prefixed with the workspace so two workspaces may
// both have "google".
func FullConnectorID(workspace, id string) string {
	if workspace == "" {
		return id
	}
	return workspacePrefix + workspace + "-" + id
}

// ConnectorSpec is what the CLI collects for a new connector.
type ConnectorSpec struct {
	Type string
	ID   string
	Name string
	// Workspace scopes the connector to one workspace's login page; empty
	// is the platform's, offered everywhere.
	Workspace    string
	ClientID     string
	ClientSecret string
	// Org limits GitHub sign-in to members of one organisation (and loads
	// its teams as groups); HostedDomain limits Google to one workspace.
	Org          string
	HostedDomain string
	// Tenant limits Microsoft sign-in to one Entra tenant (id or domain);
	// empty accepts any work or school account.
	Tenant string
	// Issuer is a generic OpenID Connect provider's issuer URL (Okta,
	// Keycloak, Auth0, ...).
	Issuer string
}

// microsoftConfig mirrors Dex's microsoft connector configuration.
type microsoftConfig struct {
	ClientID     string `json:"clientID"`
	ClientSecret string `json:"clientSecret"`
	RedirectURI  string `json:"redirectURI"`
	Tenant       string `json:"tenant,omitempty"`
	// Groups arrive only with the tenant's consent; on by default.
	UseGroupsAsWhitelist bool `json:"useGroupsAsWhitelist,omitempty"`
}

// oidcConfig mirrors Dex's oidc connector configuration.
type oidcConfig struct {
	Issuer                    string   `json:"issuer"`
	ClientID                  string   `json:"clientID"`
	ClientSecret              string   `json:"clientSecret"`
	RedirectURI               string   `json:"redirectURI"`
	Scopes                    []string `json:"scopes,omitempty"`
	InsecureEnableGroups      bool     `json:"insecureEnableGroups,omitempty"`
	GetUserInfo               bool     `json:"getUserInfo,omitempty"`
	UserNameKey               string   `json:"userNameKey,omitempty"`
	InsecureSkipEmailVerified bool     `json:"insecureSkipEmailVerified,omitempty"`
}

// githubConfig mirrors Dex's github connector configuration.
type githubConfig struct {
	ClientID      string      `json:"clientID"`
	ClientSecret  string      `json:"clientSecret"`
	RedirectURI   string      `json:"redirectURI"`
	Orgs          []githubOrg `json:"orgs,omitempty"`
	LoadAllGroups bool        `json:"loadAllGroups,omitempty"`
	TeamNameField string      `json:"teamNameField,omitempty"`
}

type githubOrg struct {
	Name string `json:"name"`
}

// googleConfig mirrors Dex's google connector configuration (groups need a
// service account and stay configuration only, RFC-0058).
type googleConfig struct {
	ClientID      string   `json:"clientID"`
	ClientSecret  string   `json:"clientSecret"`
	RedirectURI   string   `json:"redirectURI"`
	HostedDomains []string `json:"hostedDomains,omitempty"`
}

// ConnectorStore manages Dex connectors in the system namespace.
type ConnectorStore struct {
	Dynamic   dynamic.Interface
	Namespace string
	// Issuer is Dex's external URL; its callback is <issuer>/callback.
	Issuer string
}

func (s *ConnectorStore) res() dynamic.ResourceInterface {
	return s.Dynamic.Resource(ConnectorGVR).Namespace(s.Namespace)
}

// Validate checks the spec and fills defaults (id and name from the type).
func (spec *ConnectorSpec) Validate() error {
	known := false
	for _, k := range ConnectorKinds {
		known = known || k == spec.Type
	}
	if !known {
		return fmt.Errorf("unknown connector type %q (known: %s)", spec.Type, strings.Join(ConnectorKinds, ", "))
	}
	if spec.ID == "" {
		spec.ID = spec.Type
	}
	if !connectorIDRe.MatchString(spec.ID) || spec.ID == "local" {
		return fmt.Errorf("invalid connector id %q: lowercase letters, digits and dashes", spec.ID)
	}
	if spec.Workspace == "" && strings.HasPrefix(spec.ID, workspacePrefix) {
		return fmt.Errorf("connector ids starting with %q belong to workspaces", workspacePrefix)
	}
	if len(FullConnectorID(spec.Workspace, spec.ID)) > 63 {
		return errors.New("connector id too long")
	}
	if spec.Name == "" {
		spec.Name = map[string]string{"github": "GitHub", "google": "Google", "microsoft": "Microsoft", "oidc": "Single sign-on"}[spec.Type]
	}
	if strings.TrimSpace(spec.ClientID) == "" || strings.TrimSpace(spec.ClientSecret) == "" {
		return errors.New("client id and client secret are required")
	}
	if spec.Type == "oidc" {
		if !strings.HasPrefix(spec.Issuer, "https://") {
			return errors.New("an OpenID Connect provider needs its issuer URL (https://...)")
		}
	}
	return nil
}

func (s *ConnectorStore) config(spec ConnectorSpec) ([]byte, error) {
	redirect := strings.TrimRight(s.Issuer, "/") + "/callback"
	switch spec.Type {
	case "github":
		cfg := githubConfig{ClientID: spec.ClientID, ClientSecret: spec.ClientSecret, RedirectURI: redirect, TeamNameField: "slug"}
		if spec.Org != "" {
			cfg.Orgs = []githubOrg{{Name: spec.Org}}
		} else {
			cfg.LoadAllGroups = true
		}
		return json.Marshal(cfg)
	case "google":
		cfg := googleConfig{ClientID: spec.ClientID, ClientSecret: spec.ClientSecret, RedirectURI: redirect}
		if spec.HostedDomain != "" {
			cfg.HostedDomains = []string{spec.HostedDomain}
		}
		return json.Marshal(cfg)
	case "microsoft":
		cfg := microsoftConfig{ClientID: spec.ClientID, ClientSecret: spec.ClientSecret, RedirectURI: redirect, Tenant: spec.Tenant}
		return json.Marshal(cfg)
	case "oidc":
		cfg := oidcConfig{Issuer: strings.TrimRight(spec.Issuer, "/"), ClientID: spec.ClientID, ClientSecret: spec.ClientSecret, RedirectURI: redirect,
			Scopes: []string{"openid", "email", "profile", "groups"}, InsecureEnableGroups: true, GetUserInfo: true}
		return json.Marshal(cfg)
	}
	return nil, fmt.Errorf("unknown connector type %q", spec.Type)
}

// Add creates or replaces a connector.
func (s *ConnectorStore) Add(ctx context.Context, spec ConnectorSpec) (existed bool, err error) {
	if err := spec.Validate(); err != nil {
		return false, err
	}
	if s.Issuer == "" {
		return false, errors.New("the issuer URL (SHPYRD_AUTH_URL) is unknown; is auth-local enabled?")
	}
	raw, err := s.config(spec)
	if err != nil {
		return false, err
	}
	full := FullConnectorID(spec.Workspace, spec.ID)
	labels := map[string]interface{}{"app.kubernetes.io/managed-by": "shpyrd"}
	if spec.Workspace != "" {
		labels[LabelWorkspace] = spec.Workspace
	}
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": ConnectorGVR.Group + "/" + ConnectorGVR.Version,
		"kind":       "Connector",
		"metadata": map[string]interface{}{
			"name": full, "namespace": s.Namespace,
			"labels": labels,
		},
		"id":   full,
		"type": spec.Type,
		"name": spec.Name,
		// Dex declares config as []byte: base64 of the JSON.
		"config": base64.StdEncoding.EncodeToString(raw),
	}}
	if _, err := s.res().Create(ctx, obj, metav1.CreateOptions{}); err == nil {
		return false, nil
	} else if !apierrors.IsAlreadyExists(err) {
		return false, wrap(err)
	}
	cur, err := s.res().Get(ctx, full, metav1.GetOptions{})
	if err != nil {
		return true, wrap(err)
	}
	obj.SetResourceVersion(cur.GetResourceVersion())
	_, err = s.res().Update(ctx, obj, metav1.UpdateOptions{})
	return true, wrap(err)
}

// List returns every connector sorted by id, without their secrets: the
// platform's and every workspace's (Workspace says whose).
func (s *ConnectorStore) List(ctx context.Context) ([]Connector, error) {
	list, err := s.res().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, wrap(err)
	}
	out := make([]Connector, 0, len(list.Items))
	for _, u := range list.Items {
		c := connectorOf(u)
		if c.ID == "" || c.Type == "local" {
			continue // Dex's own static connector
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FullID < out[j].FullID })
	return out, nil
}

// ListFor returns one scope's connectors: the platform's ("") or a
// workspace's.
func (s *ConnectorStore) ListFor(ctx context.Context, workspace string) ([]Connector, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Connector, 0, len(all))
	for _, c := range all {
		if c.Workspace == workspace {
			out = append(out, c)
		}
	}
	return out, nil
}

func connectorOf(u unstructured.Unstructured) Connector {
	full, _, _ := unstructured.NestedString(u.Object, "id")
	typ, _, _ := unstructured.NestedString(u.Object, "type")
	name, _, _ := unstructured.NestedString(u.Object, "name")
	ws := u.GetLabels()[LabelWorkspace]
	id := full
	if ws != "" {
		id = strings.TrimPrefix(full, workspacePrefix+ws+"-")
	}
	c := Connector{ID: id, FullID: full, Type: typ, Name: name, Workspace: ws}
	if enc, _, _ := unstructured.NestedString(u.Object, "config"); enc != "" {
		if raw, err := base64.StdEncoding.DecodeString(enc); err == nil {
			var cfg struct {
				Orgs          []githubOrg `json:"orgs"`
				HostedDomains []string    `json:"hostedDomains"`
				Tenant        string      `json:"tenant"`
				Issuer        string      `json:"issuer"`
			}
			_ = json.Unmarshal(raw, &cfg)
			switch {
			case len(cfg.Orgs) > 0:
				c.Detail = "organisation " + cfg.Orgs[0].Name
			case len(cfg.HostedDomains) > 0:
				c.Detail = "domain " + cfg.HostedDomains[0]
			case cfg.Tenant != "":
				c.Detail = "tenant " + cfg.Tenant
			case cfg.Issuer != "":
				c.Detail = cfg.Issuer
			}
		}
	}
	return c
}

// Remove deletes a connector of a scope (the platform's when workspace is
// "").
func (s *ConnectorStore) Remove(ctx context.Context, workspace, id string) error {
	if workspace == "" && strings.HasPrefix(id, workspacePrefix) {
		return fmt.Errorf("connector %q belongs to a workspace; remove it from that workspace's Sign-in page", id)
	}
	err := s.res().Delete(ctx, FullConnectorID(workspace, id), metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("no connector %q; see `shpyrd auth connector list`", id)
	}
	return wrap(err)
}
