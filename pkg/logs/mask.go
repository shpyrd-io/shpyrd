package logs

import (
	"bufio"
	"errors"
	"io"
	"net/url"
	"sort"
	"strings"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// A build sees the project's secret variables (#54), and what it prints
// lands in its log, which everyone who can view the project reads. The
// build's log is served masked, as GitHub Actions masks its secrets: every
// occurrence of a secret value becomes Mask. A value shorter than
// MaskMinLength is left alone, or masking "1" or "true" would mask every 1
// and every true of the log. A value changed into another form (base64, a
// part of it) is not recognised.
const (
	Mask          = "***"
	MaskMinLength = 8
)

// Masker replaces secret values in log lines. A nil Masker masks nothing.
type Masker struct{ r *strings.Replacer }

// NewMasker masks the values given: each one whole, each line of a value
// of several lines (a private key), and the password of a URL (the URL
// printed in another form, or the password alone), when long enough.
func NewMasker(values []string) *Masker {
	seen := map[string]bool{}
	var secrets []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if len(s) >= MaskMinLength && !seen[s] {
			seen[s] = true
			secrets = append(secrets, s)
		}
	}
	for _, v := range values {
		add(v)
		if strings.Contains(v, "\n") {
			for _, line := range strings.Split(v, "\n") {
				add(line)
			}
		}
		if u, err := url.Parse(strings.TrimSpace(v)); err == nil && u.User != nil {
			if p, ok := u.User.Password(); ok {
				add(p) // decoded
			}
			// And as the URL writes it, escaped.
			if i := strings.Index(v, "://"); i >= 0 {
				if at := strings.Index(v[i+3:], "@"); at >= 0 {
					if _, raw, ok := strings.Cut(v[i+3:i+3+at], ":"); ok {
						add(raw)
					}
				}
			}
		}
	}
	if len(secrets) == 0 {
		return nil
	}
	// The longest first: a value that contains another is masked whole.
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	pairs := make([]string, 0, 2*len(secrets))
	for _, s := range secrets {
		pairs = append(pairs, s, Mask)
	}
	return &Masker{r: strings.NewReplacer(pairs...)}
}

// BuildSecrets are the values to mask in a build's log: the build Secret's
// (<app>-build-env), except the binding's own entries and the plain env: of
// shpyrd.yaml, which are not secrets.
func BuildSecrets(app *shpyrdv1.App, data map[string][]byte) []string {
	plain := map[string]bool{"type": true, "provider": true}
	for _, e := range app.Spec.Env {
		if e.ValueFrom == nil {
			plain[e.Name] = true
		}
	}
	var out []string
	for k, v := range data {
		if !plain[k] {
			out = append(out, string(v))
		}
	}
	return out
}

// Line masks one line.
func (m *Masker) Line(s string) string {
	if m == nil {
		return s
	}
	return m.r.Replace(s)
}

// Copy copies src to dst a line at a time, masked: a value split between
// two reads of the stream is still found, and each line is written (and,
// for a streaming writer, sent) as soon as it is complete.
func (m *Masker) Copy(dst io.Writer, src io.Reader) error {
	if m == nil {
		_, err := io.Copy(dst, src)
		return err
	}
	br := bufio.NewReader(src)
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			if _, werr := io.WriteString(dst, m.Line(line)); werr != nil {
				return werr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
