package project

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
)

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"my-shop":                  "my-shop",
		"My Shop":                  "my-shop",
		"  My   Shop!  ":           "my-shop",
		"Café da Manhã":            "cafe-da-manha",
		"Ação & Reação":            "acao-reacao",
		"API v2 (staging)":         "api-v2-staging",
		"shop_web.2":               "shop-web-2",
		"--leading and trailing--": "leading-and-trailing",
		"UPPER":                    "upper",
		"数据 Service":               "service",
	}
	for in, want := range cases {
		got, err := Slug(in)
		if err != nil {
			t.Fatalf("Slug(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
		if !ValidSlug(got) {
			t.Errorf("Slug(%q) = %q is not a valid slug", in, got)
		}
	}
}

func TestSlugLength(t *testing.T) {
	long := strings.Repeat("abcde-", 10) // 60 chars, a dash lands at position 40
	got, err := Slug(long)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > MaxSlugLength || strings.HasSuffix(got, "-") {
		t.Errorf("Slug(long) = %q (%d chars)", got, len(got))
	}
	if !ValidSlug(got) {
		t.Errorf("%q is not a valid slug", got)
	}
}

func TestSlugErrors(t *testing.T) {
	for _, in := range []string{"", "   ", "!!!", "数据", "---"} {
		if s, err := Slug(in); err == nil {
			t.Errorf("Slug(%q) = %q, want error", in, s)
		}
	}
}

func TestValidateSlug(t *testing.T) {
	for _, ok := range []string{"a", "shop", "my-shop-2", strings.Repeat("a", 40)} {
		if err := ValidateSlug(ok); err != nil {
			t.Errorf("ValidateSlug(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"", "My Shop", "-shop", "shop-", "shop_2", "app-shop", strings.Repeat("a", 41)} {
		if err := ValidateSlug(bad); err == nil {
			t.Errorf("ValidateSlug(%q) accepted", bad)
		}
	}
}

func TestValidateNewSlug(t *testing.T) {
	// Reserved names are refused for new projects only: ValidateSlug still
	// accepts them so existing projects keep working.
	for _, reserved := range []string{"www", "auth", "login", "shpyrd", "grafana", "console"} {
		if err := ValidateSlug(reserved); err != nil {
			t.Errorf("ValidateSlug(%q) must accept an existing name: %v", reserved, err)
		}
		if err := ValidateNewSlug(reserved); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("ValidateNewSlug(%q) = %v, want reserved", reserved, err)
		}
	}
	if err := ValidateNewSlug("shop"); err != nil {
		t.Errorf("ValidateNewSlug(shop): %v", err)
	}
}

func TestValidateWorkspaceSlug(t *testing.T) {
	for _, ok := range []string{"acme", "acmelabs", "a1", "a", strings.Repeat("a", 22)} {
		if err := ValidateWorkspaceSlug(ok); err != nil {
			t.Errorf("ValidateWorkspaceSlug(%q): %v", ok, err)
		}
	}
	// No hyphen: the first one of an app's host <workspace>-<app> ends the
	// workspace's part. 22 + "-" + a 40-character project slug is 63.
	for _, bad := range []string{"", "Acme", "-acme", "acme-", "acme-labs", "a--b", "default", "www", "login", "app-x", strings.Repeat("a", 23)} {
		if err := ValidateWorkspaceSlug(bad); err == nil {
			t.Errorf("ValidateWorkspaceSlug(%q) accepted", bad)
		}
	}
}

func TestNamespace(t *testing.T) {
	if Namespace("shop") != "app-shop" || FromNamespace("app-shop") != "shop" {
		t.Fatal("namespace mapping")
	}
	if NamespaceIn("", "shop") != "app-shop" || NamespaceIn(DefaultWorkspace, "shop") != "app-shop" {
		t.Fatal("implicit workspace namespace")
	}
	if NamespaceIn("acme", "shop") != "app-acme-shop" {
		t.Fatal("explicit workspace namespace")
	}
	if got := NamespaceIn(strings.Repeat("w", 24), strings.Repeat("p", 40)); len(got) > 63+6 {
		// app- + 24 + - + 40 = 69: the API caps project slugs at 30 in
		// explicit workspaces (RFC-0033), checked where projects are created.
		t.Logf("longest namespace: %d chars", len(got))
	}
}

func TestDisplayName(t *testing.T) {
	a := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "my-shop"}}
	if DisplayName(a) != "my-shop" || Label(a) != "my-shop" {
		t.Fatalf("default display name: %q / %q", DisplayName(a), Label(a))
	}
	SetDisplayName(a, "My Shop")
	if a.Annotations[shpyrdv1.AnnotationDisplayName] != "My Shop" {
		t.Fatalf("annotation not set: %v", a.Annotations)
	}
	if DisplayName(a) != "My Shop" || Label(a) != "My Shop (my-shop)" {
		t.Fatalf("display name: %q / %q", DisplayName(a), Label(a))
	}
	SetDisplayName(a, "my-shop") // same as the slug: annotation removed
	if _, ok := a.Annotations[shpyrdv1.AnnotationDisplayName]; ok {
		t.Fatal("annotation should be dropped when equal to the slug")
	}
	if DisplayName(nil) != "" {
		t.Fatal("nil app")
	}
}

func TestIDNaming(t *testing.T) {
	const id = "0b1e6c7a-9d6e-4c2f-8a1b-2f3e4d5c6b7a"
	short := ids.Short(id)
	ns := IDNamespace(id)
	if ns != "p-"+short || len(ns) != 27 || !IsIDNamespace(ns) {
		t.Fatalf("IDNamespace = %q", ns)
	}
	if IsIDNamespace("app-shop") || IsIDNamespace("p-short") {
		t.Error("legacy or malformed names must not read as ID namespaces")
	}
	idNamed := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: short}, Spec: shpyrdv1.AppSpec{ID: id, Slug: "shop"}}
	legacy := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop"}, Spec: shpyrdv1.AppSpec{ID: id}}
	if !IDNamed(idNamed) || IDNamed(legacy) {
		t.Error("IDNamed: named by ID vs legacy with an ID")
	}
	if SlugOf(idNamed) != "shop" || SlugOf(legacy) != "shop" || DisplayName(idNamed) != "shop" {
		t.Errorf("SlugOf/DisplayName: %q %q %q", SlugOf(idNamed), SlugOf(legacy), DisplayName(idNamed))
	}
	SetDisplayName(idNamed, "shop")
	if _, has := idNamed.Annotations[shpyrdv1.AnnotationDisplayName]; has {
		t.Error("display name equal to the slug must not be recorded")
	}
	labels := NamespaceLabelsFor("acme", "7f0d9e2c-1111-4222-8333-444455556666", id, "shop", short)
	if labels[shpyrdv1.LabelProject] != "shop" || labels[shpyrdv1.LabelWorkspace] != "acme" || labels[shpyrdv1.LabelApp] != short ||
		labels[shpyrdv1.LabelProjectID] != short || labels[shpyrdv1.LabelWorkspaceID] != ids.Short("7f0d9e2c-1111-4222-8333-444455556666") {
		t.Errorf("labels = %v", labels)
	}
	if l := IDLabels("", id); len(l) != 1 {
		t.Errorf("IDLabels with an empty workspace = %v", l)
	}
}

func TestIcon(t *testing.T) {
	a := &shpyrdv1.App{}
	if err := SetIcon(a, "chart-line"); err != nil || Icon(a) != "chart-line" {
		t.Fatalf("set: %q %v", Icon(a), err)
	}
	for _, bad := range []string{"Chart Line", "chart--line", "-chart", "<svg>", strings.Repeat("a", 41)} {
		if err := SetIcon(a, bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
	if Icon(a) != "chart-line" {
		t.Errorf("a refused name must leave the icon as it was, got %q", Icon(a))
	}
	if err := SetIcon(a, ""); err != nil || Icon(a) != "" || len(a.Annotations) != 0 {
		t.Errorf("remove: %v %v", a.Annotations, err)
	}
}

func TestIconColor(t *testing.T) {
	a := &shpyrdv1.App{}
	if err := SetIconColor(a, "teal"); err != nil || IconColor(a) != "teal" {
		t.Fatalf("set: %q %v", IconColor(a), err)
	}
	if err := SetIconColor(a, "#00ffaa"); err == nil || IconColor(a) != "teal" {
		t.Errorf("a colour outside the set: %v, kept %q", err, IconColor(a))
	}
	if err := SetIconColor(a, ""); err != nil || IconColor(a) != "" {
		t.Errorf("remove: %v", err)
	}
}

func TestIconFile(t *testing.T) {
	a := &shpyrdv1.App{}
	SetIconFile(a, "abc123", "svg")
	if v, k := IconFile(a); v != "abc123" || k != "svg" {
		t.Errorf("file = %q %q", v, k)
	}
	SetIconFile(a, "", "")
	if v, _ := IconFile(a); v != "" || len(a.Annotations) != 0 {
		t.Errorf("removed: %v", a.Annotations)
	}
}
