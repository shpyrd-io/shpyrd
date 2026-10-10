package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/spf13/cobra"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/api"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
)

func newDeployCmd(g *globalFlags) *cobra.Command {
	var (
		appName     string
		gitURL      string
		gitRef      string
		subPath     string
		image       string
		noWait      bool
		workingTree bool
		dockerfile  string
		save        bool
	)
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Build and release the current directory to a project",
		Long: `Deploy to a project.

By default the committed tree of the current directory (git HEAD, or the
whole directory outside a repository) is archived, uploaded to the cluster
and built in-cluster: with the Dockerfile when the directory has one,
otherwise with buildpacks. The resulting image is rolled out and exposed
at https://<app>.<domain>.

  shpyrd deploy --project myapp               archive the committed tree and build in-cluster
  shpyrd deploy --working-tree                archive the directory as is, uncommitted changes included
  shpyrd deploy --git https://github.com/o/r  build from a Git URL; new commits rebuild automatically (buildpacks)
  shpyrd deploy --git ... --dockerfile Dockerfile   build the repository's Dockerfile
  shpyrd deploy --image ghcr.io/o/r:tag       run a prebuilt image (no build)

The build strategy can be pinned in shpyrd.yaml (build.strategy: buildpacks
or dockerfile, plus build.dockerfile, build.target and build.env).

Before a local deploy the CLI looks at the directory for patterns the
buildpacks cannot configure on their own (a static site under public/, a
Vite app, a Rack or Rails app, PHP under public/, an Aptfile with heavy
packages) and fills in the shpyrd.yaml values they need, saying what it
inferred. Values already in shpyrd.yaml win; --save writes the inferred
ones into the file.

The project is taken from --project or from shpyrd.yaml (project: <name>).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			out := g.progress(cmd)
			name, err := resolveAppName(appName)
			if err != nil {
				return err
			}
			project, err := loadProjectConfig()
			if err != nil {
				return err
			}
			if gitURL != "" && image != "" {
				return errors.New("--git and --image are mutually exclusive")
			}
			ac, err := newAppClient(g, out)
			if err != nil {
				return err
			}
			// Everything goes through the API (RFC-0052): the same path for a
			// login session and for a kubeconfig.
			before, err := ac.getDetail(ctx, name)
			if err != nil {
				return err
			}

			// Build strategy: shpyrd.yaml wins, then --dockerfile, then the
			// presence of a Dockerfile in the deployed directory.
			if dockerfile != "" {
				if project == nil {
					project = &projectConfig{}
				}
				if project.Build == nil {
					project.Build = &projectBuild{}
				}
				project.Build.Strategy = shpyrdv1.StrategyDockerfile
				if dockerfile != "auto" {
					project.Build.Dockerfile = dockerfile
				}
			}
			// A package of a JavaScript workspace (#145): the workspace's
			// root is what gets built.
			workspaceRoot := ""
			if image == "" && gitURL == "" && subPath == "" {
				if project == nil {
					project = &projectConfig{}
				}
				top, _ := gitOutput("rev-parse", "--show-toplevel")
				root, found, err := workspaceFor(project, top)
				if err != nil {
					return err
				}
				workspaceRoot = root
				if found != nil {
					reportWorkspace(out, found, top)
					if save {
						det := &detection{inferred: []inference{{what: "build.workspace", value: found.Package}}}
						if _, _, err := saveInferences(".", name, det, false); err != nil {
							return err
						}
					}
				}
			}
			if image == "" && gitURL == "" {
				project = detectDockerfile(project, subPath)
				// Build profiles (RFC-0067): what the directory says the
				// build needs and shpyrd.yaml does not.
				var det *detection
				project, det = applyProfiles(project, firstNonEmpty(subPath, "."))
				det.report(out)
				if save && !det.empty() {
					path, created, err := saveInferences(firstNonEmpty(subPath, "."), name, det, false)
					if err != nil {
						return err
					}
					if created {
						fmt.Fprintf(out, "    wrote %s\n", path)
					} else {
						fmt.Fprintf(out, "    added to %s\n", path)
					}
				}
			}
			req, err := project.deployRequest(&before.Spec)
			if err != nil {
				return err
			}
			req.SubPath = subPath
			// Infer from the source before starting the build, using the actual
			// workspace catalog instead of assuming the shipped sizes are unchanged.
			runtime := ""
			localSource := image == "" && gitURL == ""
			if localSource {
				build := req.Build
				if build == nil {
					build = before.Spec.Build
				}
				runtime = sourceRuntime(firstNonEmpty(subPath, "."), build)
			} else if gitURL != "" {
				runtime = before.Status.Runtime
			}
			if runtime != "" {
				raw, err := ac.serverRequest(ctx, "GET", "api/sizes", nil, "")
				if err != nil {
					return err
				}
				var catalog sizes.Catalog
				if err := json.Unmarshal(raw, &catalog); err != nil {
					return fmt.Errorf("decode sizes: %w", err)
				}
				if err := catalog.Validate(); err != nil {
					return err
				}
				if localSource {
					if err := prepareRuntimeSize(&req, before.Spec, runtime, catalog, out); err != nil {
						return err
					}
				} else {
					procs := req.Processes
					if procs == nil {
						procs = before.Spec.Processes
					}
					if err := warnRuntimeMemory(procs, runtime, catalog, out); err != nil {
						return err
					}
				}
			}

			switch {
			case image != "":
				fmt.Fprintf(out, "==> Deploying prebuilt image %s to %s\n", image, name)
				req.Image = image
				req.Note = "Deploy image " + image
			case gitURL != "":
				ref := firstNonEmpty(gitRef, "main")
				fmt.Fprintf(out, "==> Deploying %s @ %s to %s\n", gitURL, ref, name)
				req.Git = &shpyrdv1.GitSource{URL: gitURL, Revision: ref}
			default:
				archive, ref, err := archiveSource(out, workingTree, workspaceRoot)
				if err != nil {
					return err
				}
				if b := req.Build; b != nil && b.Strategy == shpyrdv1.StrategyDockerfile {
					fmt.Fprintf(out, "==> Building with Dockerfile (%s)\n", firstNonEmpty(b.Dockerfile, "Dockerfile"))
				} else {
					fmt.Fprintln(out, "==> Building with buildpacks")
				}
				// An Aptfile: system packages through the .deb buildpack
				// (RFC-0065), translated into the archive's project.toml.
				if req.Build == nil || req.Build.Strategy != shpyrdv1.StrategyDockerfile {
					patched, packages, unsupported, err := withSystemPackages(archive)
					if err != nil {
						return fmt.Errorf("Aptfile: %w", err)
					}
					for _, u := range unsupported {
						fmt.Fprintf(out, "    Aptfile: %q is not supported (only package names); skipped\n", u)
					}
					if len(packages) > 0 {
						archive = patched
						if req.Build == nil {
							req.Build = &shpyrdv1.Build{}
						}
						req.Build.SystemPackages = true
						fmt.Fprintf(out, "==> System packages from Aptfile: %s\n", strings.Join(packages, ", "))
					}
				}
				fmt.Fprintf(out, "==> Uploading source (%s)\n", humanBytes(len(archive)))
				info, err := ac.uploadSourceAPI(ctx, archive)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "    archive %s\n", info.SHA256[:12])
				req.Blob = &shpyrdv1.BlobSource{URL: info.URL, SHA256: info.SHA256, Ref: ref}
			}
			body, _ := json.Marshal(req)
			raw, err := ac.serverRequest(ctx, "POST", "api/projects/"+name+"/deploy", body, "application/json")
			if err != nil {
				return err
			}
			var after api.AppDetail
			if err := json.Unmarshal(raw, &after); err != nil {
				return fmt.Errorf("deploy: %w", err)
			}
			if noWait {
				return g.print(cmd, map[string]any{"project": name, "waited": false}, func(w io.Writer) {
					fmt.Fprintln(w, "Deploy requested. Follow with `shpyrd projects info", name+"`.")
				})
			}

			specChanged := after.Status.Generation != before.Status.Generation
			// A build happens only when what is built changed: the same
			// archive with new processes or mounts is released as it is.
			sourceChanged := before.Status.LatestBuild == "" || !sameSource(before.Spec.Source, after.Spec.Source) || (before.Spec.Build == nil) != (after.Spec.Build == nil) || (before.Spec.Build != nil && after.Spec.Build != nil && !reflect.DeepEqual(*before.Spec.Build, *after.Spec.Build))
			switch {
			case after.Spec.Source != nil && after.Spec.PinnedImage == "" && req.Image == "" && sourceChanged:
				fmt.Fprintln(out, "==> Building")
				if err := ac.followBuildAPI(ctx, name, before.Status.LatestBuild, 3*time.Minute); err != nil {
					return err // the platform's message says what failed
				}
			case specChanged:
				fmt.Fprintln(out, "    source unchanged since the last build; releasing the configuration change")
			default:
				fmt.Fprintln(out, "    no changes since the last deploy")
			}

			fmt.Fprintln(out, "==> Releasing")
			final, err := ac.waitRunningAPI(ctx, name, after.Status.Generation, 10*time.Minute)
			if err != nil {
				return err
			}
			result := map[string]any{"project": name, "waited": true, "url": final.Status.URL}
			var rel *api.ReleaseView
			if n := len(final.Status.Releases); n > 0 {
				latest := final.Status.Releases[n-1]
				for _, r := range final.Status.Releases {
					if r.Number > latest.Number {
						latest = r
					}
				}
				rel = &latest
				result["release"] = latest
			}
			return g.print(cmd, result, func(w io.Writer) {
				if rel != nil {
					fmt.Fprintf(w, "\nReleased v%d: %s\n", rel.Number, rel.Description)
				}
				if final.Status.URL != "" {
					fmt.Fprintf(w, "%s\n", final.Status.URL)
				}
			})
		},
	}
	cmd.Flags().StringVar(&appName, "project", "", "project name (default from shpyrd.yaml)")
	cmd.Flags().StringVarP(&appName, "app", "a", "", "alias of --project")
	_ = cmd.Flags().MarkHidden("app")
	cmd.Flags().StringVar(&gitURL, "git", "", "build from this Git repository instead of the local checkout")
	cmd.Flags().StringVar(&gitRef, "ref", "", "Git branch, tag or commit for --git (default main)")
	cmd.Flags().StringVar(&subPath, "path", "", "directory inside the source that holds the code")
	cmd.Flags().StringVar(&image, "image", "", "deploy a prebuilt image instead of building")
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "return immediately instead of following the build and rollout")
	cmd.Flags().BoolVar(&workingTree, "working-tree", false, "archive the directory as it is on disk instead of the committed HEAD")
	cmd.Flags().StringVar(&dockerfile, "dockerfile", "", "build with this Dockerfile (path relative to the deployed directory) instead of buildpacks")
	cmd.Flags().Lookup("dockerfile").NoOptDefVal = "auto"
	cmd.Flags().BoolVar(&save, "save", false, "write what the build profile or workspace detection inferred into shpyrd.yaml")
	return cmd
}

// detectDockerfile picks the dockerfile strategy for local deploys when the
// deployed directory has a Dockerfile and shpyrd.yaml does not pin a
// strategy. Buildpacks stay the default otherwise.
func detectDockerfile(project *projectConfig, subPath string) *projectConfig {
	if project != nil && project.Build != nil && project.Build.Strategy != "" {
		return project
	}
	if project == nil {
		project = &projectConfig{}
	}
	if project.Build == nil {
		project.Build = &projectBuild{}
	}
	file := firstNonEmpty(project.Build.Dockerfile, "Dockerfile")
	if _, err := os.Stat(filepath.Join(subPath, file)); err == nil {
		project.Build.Strategy = shpyrdv1.StrategyDockerfile
	} else {
		project.Build.Strategy = shpyrdv1.StrategyBuildpacks
	}
	return project
}

// archiveSource returns a tar.gz of dir ("" for the current folder; a
// JavaScript workspace's root, #145) and a reference for the release
// description. Inside a Git repository the committed HEAD tree of dir is
// used unless workingTree is set (or dir has no tracked files); elsewhere
// the folder is tarred.
func archiveSource(out io.Writer, workingTree bool, dir string) ([]byte, string, error) {
	tarDir := firstNonEmpty(dir, ".")
	// --show-prefix is the current directory relative to the repository root
	// ("" at the root, "sub/dir/" below). `git archive HEAD` run inside a
	// subdirectory archives just that subtree with paths relative to it.
	prefix, err := gitOutputIn(dir, "rev-parse", "--show-prefix")
	if err != nil {
		fmt.Fprintln(out, "==> Archiving current directory (not a git repository)")
		data, err := tarDirectory(tarDir)
		return data, "", err
	}
	commit, _ := gitOutputIn(dir, "rev-parse", "--short=12", "HEAD")
	if tracked, _ := gitOutputIn(dir, "ls-files", "--", "."); tracked == "" && !workingTree {
		fmt.Fprintln(out, "    nothing here is committed yet; archiving the working tree instead")
		workingTree = true
	}
	if workingTree {
		fmt.Fprintf(out, "==> Archiving working tree (%s)\n", firstNonEmpty(commit, "uncommitted"))
		data, err := tarDirectory(tarDir)
		if commit != "" {
			commit += "-dirty"
		}
		return data, commit, err
	}
	if dirty, _ := gitOutputIn(dir, "status", "--porcelain", "--", "."); dirty != "" {
		fmt.Fprintln(out, "    warning: uncommitted changes are not included (use --working-tree to deploy them)")
	}
	what := "HEAD"
	if p := strings.TrimSuffix(prefix, "/"); p != "" {
		what = "HEAD:" + p
	}
	fmt.Fprintf(out, "==> Archiving %s (%s)\n", what, commit)
	cmd := exec.Command("git", "archive", "--format=tar.gz", "HEAD")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, "", fmt.Errorf("git archive: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return data, commit, nil
}

func gitOutput(args ...string) (string, error) { return gitOutputIn("", args...) }

// gitOutputIn runs git in dir ("" for the current folder).
func gitOutputIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stderr = io.Discard
	b, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// tarDirectory archives dir (excluding .git and node_modules) as tar.gz.
func tarDirectory(dir string) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		base := filepath.Base(rel)
		if info.IsDir() && (base == ".git" || base == "node_modules") {
			return filepath.SkipDir
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// sameSource reports whether two sources build the same thing: the same
// archive (by digest, else URL), the same Git URL and revision, the same
// directory.
func sameSource(a, b *shpyrdv1.Source) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.SubPath != b.SubPath {
		return false
	}
	switch {
	case a.Blob != nil && b.Blob != nil:
		if a.Blob.SHA256 != "" && b.Blob.SHA256 != "" {
			return a.Blob.SHA256 == b.Blob.SHA256
		}
		return a.Blob.URL == b.Blob.URL
	case a.Git != nil && b.Git != nil:
		return a.Git.URL == b.Git.URL && a.Git.Revision == b.Git.Revision
	}
	return false
}
