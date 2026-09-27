package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	yaml "sigs.k8s.io/yaml/goyaml.v3"
)

// saveInferences writes what the profiles inferred into shpyrd.yaml in
// dir: a new file with the project name, or the existing file with the
// missing keys added in place, comments and order kept. setProject makes
// the project key say projectName even in an existing file (a project
// just created for this directory). It returns the path written and
// whether the file is new.
func saveInferences(dir, projectName string, det *detection, setProject bool) (string, bool, error) {
	path := filepath.Join(dir, "shpyrd.yaml")
	var doc yaml.Node
	created := false
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		created = true
		if projectName == "" {
			return "", false, errors.New("no shpyrd.yaml to add to and no project name to start one")
		}
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
		setPath(doc.Content[0], []string{"project"}, scalar(projectName))
	case err != nil:
		return "", false, err
	default:
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return "", false, fmt.Errorf("shpyrd.yaml: %w", err)
		}
		if len(doc.Content) == 0 {
			doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
		}
		if doc.Content[0].Kind != yaml.MappingNode {
			return "", false, errors.New("shpyrd.yaml: expected a mapping at the top level")
		}
	}
	root := doc.Content[0]
	if setProject && !created && projectName != "" {
		overrideScalar(root, "project", projectName)
	}
	for _, inf := range det.inferred {
		switch {
		case inf.what == "build.buildpacks":
			seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
			for _, bp := range strings.Split(strings.Trim(inf.value, "[]"), ",") {
				seq.Content = append(seq.Content, scalar(strings.TrimSpace(bp)))
			}
			setPath(root, []string{"build", "buildpacks"}, seq)
		case inf.what == "build.stack":
			setPath(root, []string{"build", "stack"}, scalar(inf.value))
		case strings.HasPrefix(inf.what, "env."):
			setPath(root, []string{"env", strings.TrimPrefix(inf.what, "env.")}, scalar(inf.value))
		case inf.what == "processes.web.healthCheck.path":
			setPath(root, []string{"processes", "web", "healthCheck", "path"}, scalar(inf.value))
		default: // a build variable
			setPath(root, []string{"build", "env", inf.what}, scalar(inf.value))
		}
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return "", false, err
	}
	if err := enc.Close(); err != nil {
		return "", false, err
	}
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		return "", false, err
	}
	return path, created, nil
}

// scalar is a plain YAML scalar; values that YAML would read as something
// other than a string ("true", "1") are quoted so they stay strings.
func scalar(v string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Value: v}
	switch strings.ToLower(v) {
	case "true", "false", "yes", "no", "on", "off", "null", "~", "":
		n.Style = yaml.DoubleQuotedStyle
	default:
		if isNumeric(v) {
			n.Style = yaml.DoubleQuotedStyle
		}
	}
	return n
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	dot := false
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r == '.' && !dot && i > 0:
			dot = true
		case (r == '-' || r == '+') && i == 0 && len(s) > 1:
		default:
			return false
		}
	}
	return true
}

// overrideScalar sets a top-level key to value, replacing what is there.
func overrideScalar(m *yaml.Node, key, value string) {
	for i := 0; i < len(m.Content)-1; i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = scalar(value)
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, scalar(value))
}

// setPath sets key path under a mapping node, creating mappings on the
// way and leaving an existing value alone.
func setPath(m *yaml.Node, path []string, value *yaml.Node) {
	for i := 0; i < len(m.Content)-1; i += 2 {
		if m.Content[i].Value != path[0] {
			continue
		}
		if len(path) == 1 {
			return // set already: shpyrd.yaml wins
		}
		child := m.Content[i+1]
		if child.Kind != yaml.MappingNode {
			return // not a mapping we can descend into
		}
		setPath(child, path[1:], value)
		return
	}
	key := &yaml.Node{Kind: yaml.ScalarNode, Value: path[0]}
	if len(path) == 1 {
		m.Content = append(m.Content, key, value)
		return
	}
	child := &yaml.Node{Kind: yaml.MappingNode}
	m.Content = append(m.Content, key, child)
	setPath(child, path[1:], value)
}
