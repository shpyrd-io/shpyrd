package gitauth

import "testing"

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		in, scheme, host, port, path, https, ssh string
	}{
		{"https://github.com/acme/api", "https", "github.com", "", "acme/api", "https://github.com/acme/api.git", "git@github.com:acme/api.git"},
		{"https://github.com/acme/api.git", "https", "github.com", "", "acme/api", "https://github.com/acme/api.git", "git@github.com:acme/api.git"},
		{"git@github.com:acme/api.git", "ssh", "github.com", "", "acme/api", "https://github.com/acme/api.git", "git@github.com:acme/api.git"},
		{"ssh://git@git.example.com:2222/team/app.git", "ssh", "git.example.com", "2222", "team/app", "https://git.example.com/team/app.git", "ssh://git@git.example.com:2222/team/app.git"},
		{"http://gitea.local:3000/me/site", "http", "gitea.local", "3000", "me/site", "http://gitea.local:3000/me/site.git", "git@gitea.local:me/site.git"},
		{"https://gitlab.com/group/sub/project", "https", "gitlab.com", "", "group/sub/project", "https://gitlab.com/group/sub/project.git", "git@gitlab.com:group/sub/project.git"},
	} {
		r, err := Parse(tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if r.Scheme != tc.scheme || r.Host != tc.host || r.Port != tc.port || r.Path != tc.path {
			t.Errorf("%s: got %+v", tc.in, r)
		}
		if got := r.HTTPS(); got != tc.https {
			t.Errorf("%s: HTTPS() = %s, want %s", tc.in, got, tc.https)
		}
		if got := r.SSH(); got != tc.ssh {
			t.Errorf("%s: SSH() = %s, want %s", tc.in, got, tc.ssh)
		}
	}
}

func TestParseRefuses(t *testing.T) {
	for _, in := range []string{"", "github.com/acme/api", "ftp://x/y", "https://github.com/", "git@github.com"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("%q: want an error", in)
		}
	}
}
