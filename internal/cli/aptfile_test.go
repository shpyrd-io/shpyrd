package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
)

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	zw.Close()
	return buf.Bytes()
}

func readTarGz(t *testing.T, data []byte) map[string]string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		out[hdr.Name] = string(b)
	}
	return out
}

func TestAptfile(t *testing.T) {
	// No Aptfile: untouched.
	plain := tarGz(t, map[string]string{"Gemfile": "source 'https://rubygems.org'"})
	out, pkgs, _, err := withSystemPackages(plain)
	if err != nil || pkgs != nil || !bytes.Equal(out, plain) {
		t.Fatalf("plain archive changed: %v %v", pkgs, err)
	}
	// An Aptfile with comments and a repo line: project.toml is written.
	arch := tarGz(t, map[string]string{"Aptfile": "# image libs\nlibglib2.0-0\nlibvips42\n\n:repo:deb http://x y\n", "Gemfile": ""})
	out, pkgs, unsupported, err := withSystemPackages(arch)
	if err != nil || strings.Join(pkgs, ",") != "libglib2.0-0,libvips42" || len(unsupported) != 1 {
		t.Fatalf("aptfile: %v %v %v", pkgs, unsupported, err)
	}
	files := readTarGz(t, out)
	toml := files["project.toml"]
	if !strings.Contains(toml, `schema-version = "0.2"`) || !strings.Contains(toml, `[com.heroku.buildpacks.deb-packages]`) || !strings.Contains(toml, `install = ["libglib2.0-0", "libvips42"]`) {
		t.Errorf("project.toml = %q", toml)
	}
	if files["Aptfile"] == "" || files["Gemfile"] != "" {
		t.Errorf("other files disturbed: %v", files)
	}
	// An existing project.toml keeps its content and gains the section.
	arch = tarGz(t, map[string]string{"Aptfile": "libvips42\n", "project.toml": "[_]\nschema-version = \"0.2\"\n[[io.buildpacks.build.env]]\nname = \"X\"\nvalue = \"1\"\n"})
	out, _, _, err = withSystemPackages(arch)
	if err != nil {
		t.Fatal(err)
	}
	toml = readTarGz(t, out)["project.toml"]
	if !strings.Contains(toml, `name = "X"`) || !strings.Contains(toml, `install = ["libvips42"]`) || strings.Count(toml, "schema-version") != 1 {
		t.Errorf("merged project.toml = %q", toml)
	}
	// A project.toml that already declares the section is left alone.
	arch = tarGz(t, map[string]string{"Aptfile": "libvips42\n", "project.toml": "[com.heroku.buildpacks.deb-packages]\ninstall = [\"other\"]\n"})
	out, pkgs, _, _ = withSystemPackages(arch)
	if !bytes.Equal(out, arch) || len(pkgs) != 1 {
		t.Error("an explicit section must win")
	}
}
