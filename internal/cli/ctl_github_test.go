package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// onCluster points shpyrd-ctl at a fake cluster installed with the
// dashboard at https://shpyrd.example.com, and returns it.
func onCluster(t *testing.T, objs ...corev1.Secret) kubernetes.Interface {
	t.Helper()
	record := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: install.InstallRecordName, Namespace: install.DefaultSystemNamespace},
		Data:       map[string]string{"vars": install.VarDashboardURL + ": https://shpyrd.example.com\n"},
	}
	cs := kubefake.NewSimpleClientset(record)
	for i := range objs {
		if _, err := cs.CoreV1().Secrets(objs[i].Namespace).Create(context.Background(), &objs[i], metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	prev := ctlKube
	ctlKube = func(*globalFlags) (*kube.Client, error) {
		return &kube.Client{Kube: cs, Namespace: install.DefaultSystemNamespace}, nil
	}
	t.Cleanup(func() { ctlKube = prev })
	return cs
}

func runCtl(t *testing.T, args ...string) (string, error) {
	t.Helper()
	defer func(prev bool) { preferKubeconfig = prev }(preferKubeconfig)
	root := NewCtl()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

var formAction = regexp.MustCompile(`action="([^"]+)"`)
var manifestValue = regexp.MustCompile(`name="manifest" value="([^"]*)"`)

func TestCtlGitHubAppCreateRegistersTheAppFromItsManifest(t *testing.T) {
	fake := githubtest.NewServer(t)
	t.Setenv(github.EnvAPIURL, fake.URL)
	cs := onCluster(t)
	hold := holdSecretCreates(cs)
	seen := make(chan error, 1)
	prevBrowse := browse
	browse = func(page string) error {
		go func() {
			seen <- actAsTheBrowser(t, page, hold, "https://github.com/organizations/acme/settings/apps/new?state=")
		}()
		return nil
	}
	t.Cleanup(func() { browse = prevBrowse })

	out, err := runCtl(t, "github", "app", "create", "--org", "acme")
	if err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}
	if err := <-seen; err != nil {
		t.Fatal(err)
	}
	sec, err := cs.CoreV1().Secrets(install.DefaultSystemNamespace).Get(context.Background(), github.SecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	app, err := github.AppFromSecret(sec)
	if err != nil || app != fake.App() {
		t.Fatalf("the Secret holds %+v, %v", app, err)
	}
	for _, want := range []string{"Registered shpyrd test", fake.App().HTMLURL, fake.App().InstallURL(), "shpyrd github connect"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, githubtest.ClientSecret) || strings.Contains(out, "PRIVATE KEY") {
		t.Errorf("a credential was printed:\n%s", out)
	}
}

// actAsTheBrowser opens the local page, checks the manifest it would send
// GitHub, then comes back as GitHub's redirect does: with another Host,
// with a state that is not the page's, with it, and with it again. It runs
// in a goroutine, so it reports through t.Errorf and its error, never a
// panic.
func actAsTheBrowser(t *testing.T, page string, hold secretHold, wantTarget string) error {
	res, err := http.Get(page)
	if err != nil {
		return err
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	actionMatch, manifestMatch := formAction.FindStringSubmatch(string(body)), manifestValue.FindStringSubmatch(string(body))
	if actionMatch == nil || manifestMatch == nil {
		return fmt.Errorf("the page has no form action or manifest:\n%s", body)
	}
	action := html.UnescapeString(actionMatch[1])
	if !strings.HasPrefix(action, wantTarget) {
		t.Errorf("form action %q, want it to start with %q", action, wantTarget)
	}
	state := strings.TrimPrefix(action, wantTarget)
	var m appManifest
	if err := json.Unmarshal([]byte(html.UnescapeString(manifestMatch[1])), &m); err != nil {
		return err
	}
	base := strings.TrimSuffix(page, "/")
	if m.Name != "shpyrd-shpyrd.example.com" || m.URL != "https://shpyrd.example.com" || m.HookAttributes.URL != "https://shpyrd.example.com/api/github/webhook" ||
		m.RedirectURL != base+"/created" || m.SetupURL != "https://shpyrd.example.com/api/github/installed" ||
		len(m.CallbackURLs) != 1 || m.CallbackURLs[0] != m.SetupURL || !m.RequestOAuthOnInstall || !m.SetupOnUpdate || m.Public {
		t.Errorf("manifest = %+v", m)
	}
	wantPerms := map[string]string{"contents": "read", "metadata": "read", "members": "read", "checks": "write", "deployments": "write", "statuses": "read"}
	for k, v := range wantPerms {
		if m.DefaultPermissions[k] != v {
			t.Errorf("permission %s = %q", k, m.DefaultPermissions[k])
		}
	}
	if strings.Join(m.DefaultEvents, ",") != "push,check_suite,status" {
		t.Errorf("events = %v", m.DefaultEvents)
	}
	for _, path := range []string{"/", "/created?code=" + githubtest.ConversionCode + "&state=" + state} {
		req, _ := http.NewRequest("GET", base+path, nil)
		req.Host = "rebound.example.com"
		if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != http.StatusForbidden {
			t.Errorf("%s with another Host: %v %v", path, res, err)
		}
	}
	if res, err := http.Get(base + "/created?code=" + githubtest.ConversionCode + "&state=not-it"); err != nil || res.StatusCode != http.StatusBadRequest {
		t.Errorf("a state that is not the page's: %v %v", res, err)
	}
	// The first good callback keeps the credentials, which waits on
	// released; a second one meanwhile is turned away with 409.
	callback := base + "/created?code=" + githubtest.ConversionCode + "&state=" + state
	first := make(chan error, 1)
	go func() {
		res, err := http.Get(callback)
		if err != nil {
			first <- err
			return
		}
		done, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK || !strings.Contains(string(done), "Registered") {
			t.Errorf("created: %d %s", res.StatusCode, done)
		}
		first <- nil
	}()
	// Once the first is keeping the Secret, send the second.
	select {
	case <-hold.entered:
	case err := <-first:
		return fmt.Errorf("the first callback ended before keeping the Secret: %v", err)
	}
	res, err = http.Get(callback)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Errorf("a second callback: %d, want 409", res.StatusCode)
	}
	close(hold.released)
	return <-first
}

// secretHold holds Secret creates: entered closes when the first starts,
// and each waits until released closes.
type secretHold struct{ entered, released chan struct{} }

func holdSecretCreates(cs kubernetes.Interface) secretHold {
	h := secretHold{make(chan struct{}), make(chan struct{})}
	var once sync.Once
	cs.(*kubefake.Clientset).PrependReactor("create", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		once.Do(func() { close(h.entered) })
		<-h.released
		return false, nil, nil
	})
	return h
}

func TestCtlGitHubAppCreateOnAPersonalAccountPostsToTheUsersSettings(t *testing.T) {
	fake := githubtest.NewServer(t)
	t.Setenv(github.EnvAPIURL, fake.URL)
	cs := onCluster(t)
	hold := holdSecretCreates(cs)
	seen := make(chan error, 1)
	prevBrowse := browse
	browse = func(page string) error {
		go func() { seen <- actAsTheBrowser(t, page, hold, "https://github.com/settings/apps/new?state=") }()
		return nil
	}
	t.Cleanup(func() { browse = prevBrowse })
	if out, err := runCtl(t, "github", "app", "create"); err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}
	if err := <-seen; err != nil {
		t.Fatal(err)
	}
	if _, err := cs.CoreV1().Secrets(install.DefaultSystemNamespace).Get(context.Background(), github.SecretName, metav1.GetOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestCtlGitHubAppCreateNeedsTheDashboardsURL(t *testing.T) {
	fake := githubtest.NewServer(t)
	t.Setenv(github.EnvAPIURL, fake.URL)
	cs := onCluster(t)
	record, err := cs.CoreV1().ConfigMaps(install.DefaultSystemNamespace).Get(context.Background(), install.InstallRecordName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	record.Data = map[string]string{}
	if _, err := cs.CoreV1().ConfigMaps(install.DefaultSystemNamespace).Update(context.Background(), record, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	prevBrowse := browse
	browse = func(string) error { t.Error("the browser must not open"); return nil }
	t.Cleanup(func() { browse = prevBrowse })
	_, err = runCtl(t, "github", "app", "create")
	if err == nil || !strings.Contains(err.Error(), "name no dashboard URL: give it with --url") {
		t.Errorf("err = %v", err)
	}
	if calls := fake.Calls(); len(calls) != 0 {
		t.Errorf("GitHub was called: %v", calls)
	}
}

func TestCtlGitHubAppCreateRefusesANameGitHubWouldRefuse(t *testing.T) {
	onCluster(t)
	prevBrowse := browse
	browse = func(string) error { t.Error("the browser must not open"); return nil }
	t.Cleanup(func() { browse = prevBrowse })
	_, err := runCtl(t, "github", "app", "create", "--name", strings.Repeat("a", 35))
	if err == nil || !strings.Contains(err.Error(), "34 characters") {
		t.Errorf("err = %v", err)
	}
}

func TestTheWaitIsSaidInMinutes(t *testing.T) {
	for wait, want := range map[time.Duration]string{10 * time.Minute: "10 minutes", time.Minute: "1 minute", 50 * time.Millisecond: "50ms"} {
		if got := waitText(wait); got != want {
			t.Errorf("waitText(%v) = %q, want %q", wait, got, want)
		}
	}
}

func TestCtlGitHubAppCreateRefusesASecondApp(t *testing.T) {
	fake := githubtest.NewServer(t)
	onCluster(t, *fake.Secret(install.DefaultSystemNamespace))
	prevBrowse := browse
	browse = func(string) error { t.Error("the browser must not open"); return nil }
	t.Cleanup(func() { browse = prevBrowse })
	_, err := runCtl(t, "github", "app", "create")
	if err == nil || !strings.Contains(err.Error(), "shpyrd-ctl github app delete") {
		t.Errorf("err = %v", err)
	}
}

func TestCtlGitHubAppCreateGivesUpAfterTheWait(t *testing.T) {
	onCluster(t)
	prevBrowse, prevWait := browse, manifestWait
	browse, manifestWait = func(string) error { return nil }, 50*time.Millisecond
	t.Cleanup(func() { browse, manifestWait = prevBrowse, prevWait })
	_, err := runCtl(t, "github", "app", "create")
	if err == nil || !strings.Contains(err.Error(), "run shpyrd-ctl github app create again") {
		t.Errorf("err = %v", err)
	}
}

func TestCtlGitHubAppShowNeverPrintsTheKeysAndDeleteRemovesTheSecret(t *testing.T) {
	fake := githubtest.NewServer(t)
	cs := onCluster(t, *fake.Secret(install.DefaultSystemNamespace))
	out, err := runCtl(t, "github", "app", "show")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{githubtest.Slug, "1234", fake.App().HTMLURL} {
		if !strings.Contains(out, want) {
			t.Errorf("show lacks %q:\n%s", want, out)
		}
	}
	for _, secret := range []string{githubtest.ClientSecret, githubtest.WebhookSecret, "PRIVATE KEY"} {
		if strings.Contains(out, secret) {
			t.Errorf("show printed a credential:\n%s", out)
		}
	}
	out, err = runCtl(t, "github", "app", "delete")
	if err != nil || !strings.Contains(out, "Delete the App on GitHub too") {
		t.Fatalf("delete: %v\n%s", err, out)
	}
	if _, err := cs.CoreV1().Secrets(install.DefaultSystemNamespace).Get(context.Background(), github.SecretName, metav1.GetOptions{}); err == nil {
		t.Error("the Secret is still there")
	}
	if _, err := runCtl(t, "github", "app", "show"); err == nil || !strings.Contains(err.Error(), "no GitHub App") {
		t.Errorf("show without one: %v", err)
	}
}

func TestTheAppsNameFitsGitHubsLimit(t *testing.T) {
	if got := appName("https://shpyrd.example.com", ""); got != "shpyrd-shpyrd.example.com" {
		t.Errorf("short: %q", got)
	}
	long := appName("https://dashboard.a-very-long-customer-name.example.com:8443", "")
	if len(long) > 34 || !strings.HasPrefix(long, "shpyrd-dashboard.") || strings.HasSuffix(long, ".") || strings.HasSuffix(long, "-") {
		t.Errorf("long: %q (%d)", long, len(long))
	}
	if got := appName("https://shpyrd.example.com", "Acme deploys"); got != "Acme deploys" {
		t.Errorf("--name: %q", got)
	}
}
