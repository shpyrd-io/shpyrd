package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strings"
)

// An Aptfile (Heroku's convention: one Debian package per line) is what a
// developer writes; the .deb packages buildpack reads project.toml. The CLI
// translates at deploy time (RFC-0065): the archive gets the project.toml
// section (and loses the Aptfile, which the buildpack would otherwise call
// deprecated), and the deploy request asks the platform to compose the
// buildpack in front of the language's. Nothing in the repository changes.
//
// Every package is installed with force: the buildpack skips a package it
// finds "already installed", but it looks at the build image, and Paketo's
// build image carries libraries the run image lacks (glib, libgomp), so a
// skipped package is missing at run time. The check still applies to the
// dependencies it pulls in, which is why a library that a listed package
// needs may have to be listed itself.

// aptfilePackages parses an Aptfile: package names, one per line; comments
// and blank lines skipped; ":repo:" lines and .deb URLs (Heroku extras the
// buildpack does not take) are reported so they are not lost silently.
func aptfilePackages(content string) (packages, unsupported []string) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, ":repo:") || strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			unsupported = append(unsupported, line)
			continue
		}
		packages = append(packages, line)
	}
	return packages, unsupported
}

// debPackagesSection is the project.toml the heroku/deb-packages buildpack
// reads; force makes it install what the build image already has.
func debPackagesSection(packages []string) string {
	quoted := make([]string, 0, len(packages))
	for _, p := range packages {
		quoted = append(quoted, fmt.Sprintf("{ name = %q, force = true }", p))
	}
	return "\n# Written by shpyrd from the Aptfile at deploy time.\n[com.heroku.buildpacks.deb-packages]\ninstall = [" + strings.Join(quoted, ", ") + "]\n"
}

// withSystemPackages returns the archive with a project.toml carrying the
// Aptfile's packages (a new file, or the section appended to the one there),
// the packages found, and whether an Aptfile was there at all. pkg is a
// JavaScript workspace's package ("" otherwise, #145): its own Aptfile
// comes first, the root's after.
func withSystemPackages(archive []byte, pkg string) ([]byte, []string, []string, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, nil, nil, err
	}
	tr := tar.NewReader(gz)
	type entry struct {
		hdr  *tar.Header
		body []byte
	}
	var entries []entry
	var aptfile, pkgAptfile, projectTOML string
	pkgName := ""
	if pkg != "" {
		pkgName = strings.TrimSuffix(pkg, "/") + "/Aptfile"
	}
	hasProject := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, nil, err
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return nil, nil, nil, err
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		switch {
		case name == "Aptfile" && hdr.Typeflag == tar.TypeReg:
			aptfile = string(body)
		case pkgName != "" && name == pkgName && hdr.Typeflag == tar.TypeReg:
			pkgAptfile = string(body)
		case name == "project.toml" && hdr.Typeflag == tar.TypeReg:
			projectTOML, hasProject = string(body), true
		}
		entries = append(entries, entry{hdr: hdr, body: body})
	}
	used := "Aptfile"
	if pkgAptfile != "" {
		aptfile, used = pkgAptfile, pkgName
	}
	if aptfile == "" {
		return archive, nil, nil, nil
	}
	packages, unsupported := aptfilePackages(aptfile)
	if len(packages) == 0 {
		return archive, nil, unsupported, nil
	}
	if strings.Contains(projectTOML, "[com.heroku.buildpacks.deb-packages]") {
		return archive, packages, unsupported, nil // the project already says it
	}
	content := projectTOML
	if !hasProject {
		content = "[_]\nschema-version = \"0.2\"\n"
	}
	content += debPackagesSection(packages)

	var out bytes.Buffer
	zw := gzip.NewWriter(&out)
	tw := tar.NewWriter(zw)
	wrote := false
	for _, e := range entries {
		if strings.TrimPrefix(e.hdr.Name, "./") == used && e.hdr.Typeflag == tar.TypeReg {
			continue // translated; its presence only draws a deprecation notice
		}
		if strings.TrimPrefix(e.hdr.Name, "./") == "project.toml" {
			e.body = []byte(content)
			e.hdr.Size = int64(len(e.body))
			wrote = true
		}
		if err := tw.WriteHeader(e.hdr); err != nil {
			return nil, nil, nil, err
		}
		if _, err := tw.Write(e.body); err != nil {
			return nil, nil, nil, err
		}
	}
	if !wrote {
		hdr := &tar.Header{Name: "project.toml", Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, nil, nil, err
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			return nil, nil, nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, nil, nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, nil, nil, err
	}
	return out.Bytes(), packages, unsupported, nil
}
