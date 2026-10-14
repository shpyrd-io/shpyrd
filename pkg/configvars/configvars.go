// Package configvars manages an app's config vars: the keys of Secret
// <app>-env plus per-key metadata (when each was last set). Values are
// write-only unless explicitly marked as plain variables.
package configvars

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// AnnotationMeta on the Secret holds {"KEY": {"updatedAt": RFC3339}}.
const AnnotationMeta = "shpyrd.io/config-meta"

var keyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Var is one config var without its value.
type Var struct {
	Name      string  `json:"name"`
	UpdatedAt string  `json:"updatedAt,omitempty"`
	Value     *string `json:"value,omitempty"`
}

type meta struct {
	Plain     bool   `json:"plain,omitempty"`
	UpdatedAt string `json:"updatedAt"`
}

// ValidateKey checks that k is a sane environment variable name.
func ValidateKey(k string) error {
	if !keyPattern.MatchString(k) {
		return fmt.Errorf("invalid config var name %q: use letters, digits and underscores, not starting with a digit", k)
	}
	return nil
}

// Apply sets and unsets keys on the Secret and records the change time.
func Apply(sec *corev1.Secret, set map[string]string, unset []string, now time.Time) error {
	if len(set) == 0 && len(unset) == 0 {
		return errors.New("nothing to change")
	}
	if sec.Data == nil {
		sec.Data = map[string][]byte{}
	}
	m := readMeta(sec)
	ts := now.UTC().Format(time.RFC3339)
	for k, v := range set {
		if err := ValidateKey(k); err != nil {
			return err
		}
		sec.Data[k] = []byte(v)
		m[k] = meta{UpdatedAt: ts}
	}
	for _, k := range unset {
		delete(sec.Data, k)
		delete(m, k)
	}
	writeMeta(sec, m)
	return nil
}

// List returns the config var names, sorted, with their metadata.
func List(sec *corev1.Secret) []Var {
	if sec == nil {
		return []Var{}
	}
	m := readMeta(sec)
	out := make([]Var, 0, len(sec.Data))
	for k := range sec.Data {
		out = append(out, Var{Name: k, UpdatedAt: m[k].UpdatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ParseDotenv parses KEY=VALUE lines (.env style: comments, blank lines and
// surrounding quotes are handled) into a map.
func ParseDotenv(text string) (map[string]string, error) {
	out := map[string]string{}
	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE", n+1)
		}
		k = strings.TrimSpace(k)
		if err := ValidateKey(k); err != nil {
			return nil, fmt.Errorf("line %d: %w", n+1, err)
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		out[k] = v
	}
	return out, nil
}

func readMeta(sec *corev1.Secret) map[string]meta {
	m := map[string]meta{}
	if raw := sec.Annotations[AnnotationMeta]; raw != "" {
		_ = json.Unmarshal([]byte(raw), &m)
	}
	return m
}

func writeMeta(sec *corev1.Secret, m map[string]meta) {
	if sec.Annotations == nil {
		sec.Annotations = map[string]string{}
	}
	if len(m) == 0 {
		delete(sec.Annotations, AnnotationMeta)
		return
	}
	b, _ := json.Marshal(m)
	sec.Annotations[AnnotationMeta] = string(b)
}

// Plain reports explicit visibility; legacy keys always remain secrets.
func Plain(sec *corev1.Secret, name string) bool { return readMeta(sec)[name].Plain }

// Public lists only explicitly public values. List continues to omit values.
func Public(sec *corev1.Secret) []Var {
	out := []Var{}
	for _, v := range List(sec) {
		if Plain(sec, v.Name) {
			value := string(sec.Data[v.Name])
			v.Value = &value
			out = append(out, v)
		}
	}
	return out
}

func Secrets(sec *corev1.Secret) []Var {
	out := []Var{}
	for _, v := range List(sec) {
		if !Plain(sec, v.Name) {
			out = append(out, v)
		}
	}
	return out
}

// SensitiveName asks for deliberate confirmation before publishing common
// credential names. Existing secrets require confirmation regardless of name.
func SensitiveName(name string) bool {
	name = strings.ToUpper(name)
	return strings.HasSuffix(name, "_KEY") || strings.HasSuffix(name, "_SECRET") || strings.HasSuffix(name, "_TOKEN") || strings.Contains(name, "PASSWORD") || name == "SECRET" || name == "TOKEN"
}

func ApplyKind(sec *corev1.Secret, set map[string]string, unset, convert []string, plain, confirm bool, now time.Time) error {
	for name := range set {
		if err := ValidateKey(name); err != nil {
			return err
		}
		_, exists := sec.Data[name]
		if plain && !confirm && (SensitiveName(name) || (exists && !Plain(sec, name))) {
			return fmt.Errorf("%s may contain a secret: confirm that its value can be visible to everyone who can view the project", name)
		}
	}
	for _, name := range convert {
		if _, exists := sec.Data[name]; !exists {
			return fmt.Errorf("config var %s does not exist", name)
		}
		if plain && !confirm {
			return fmt.Errorf("confirm that %s can be visible to everyone who can view the project", name)
		}
	}
	filtered := []string{}
	for _, name := range unset {
		if Plain(sec, name) == plain {
			filtered = append(filtered, name)
		}
	}
	if len(set)+len(filtered) > 0 {
		if err := Apply(sec, set, filtered, now); err != nil {
			return err
		}
	}
	m := readMeta(sec)
	for name := range set {
		entry := m[name]
		entry.Plain = plain
		m[name] = entry
	}
	for _, name := range convert {
		entry := m[name]
		entry.Plain = plain
		entry.UpdatedAt = now.UTC().Format(time.RFC3339)
		m[name] = entry
	}
	writeMeta(sec, m)
	return nil
}
