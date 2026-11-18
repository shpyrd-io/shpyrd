package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/api"
	"github.com/shpyrd-io/shpyrd/pkg/catalog"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

// The catalog (pkg/catalog): the applications a project can be made
// from. `list`, `show` and `install` read the workspace's catalog through
// the API; `validate` and `publish` are for the catalog repository's own
// checks and CI, and need no server.
func newCatalogCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Applications a project can be made from",
		Long: `The catalog lists applications a project can be made from: an agent, a
CRM, an open-source project that runs on the platform. An entry says where
the code is, how it runs and what it needs; installing it makes a project
with all of that, on the version of the entry the catalog published: a fixed
commit of the code. The project stays on that version until the catalog
publishes another one; then 'catalog status' says an update is available,
and 'catalog update' previews and applies it.

  shpyrd catalog list
  shpyrd catalog show fluxyr
  shpyrd catalog install fluxyr --input OPENROUTER_API_KEY=sk-or-...
  shpyrd catalog status fluxyr
  shpyrd catalog update fluxyr

The catalog is a repository of entry folders, published as an index the
server reads (SHPYRD_CATALOG_URL). 'validate' checks a folder of entries,
and 'publish' writes the index its CI serves.`,
		Args: cobra.NoArgs,
		RunE: catalogList(g),
	}
	cmd.AddCommand(newCatalogListCmd(g), newCatalogShowCmd(g), newCatalogInstallCmd(g), newCatalogStatusCmd(g), newCatalogUpdateCmd(g), newCatalogValidateCmd(g), newCatalogPublishCmd(g))
	return cmd
}

func newCatalogListCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List the entries of the catalog",
		Aliases: []string{"ls"},
		Args:    cobra.NoArgs,
		RunE:    catalogList(g),
	}
}

func catalogList(g *globalFlags) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ctx := signalContext()
		ac, err := newAppClient(g, g.progress(cmd))
		if err != nil {
			return err
		}
		raw, err := ac.serverRequest(ctx, "GET", "api/catalog", nil, "")
		if err != nil {
			return err
		}
		var view catalogListView
		if err := json.Unmarshal(raw, &view); err != nil {
			return fmt.Errorf("decode catalog: %w", err)
		}
		return g.print(cmd, view, func(w io.Writer) {
			if len(view.Entries) == 0 {
				fmt.Fprintln(w, "The catalog is empty.")
				return
			}
			tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ENTRY\tNAME\tVERSION\tCATEGORY\tNEEDS\tDESCRIPTION")
			for _, e := range view.Entries {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", e.Name, e.DisplayName, e.version(), e.Category, needsOf(e.CatalogEntryView), e.Description)
			}
			tw.Flush()
			fmt.Fprintf(w, "\nFrom %s. Install one with: shpyrd catalog install <entry>\n", view.URL)
		})
	}
}

// catalogEntry is an entry as the server answers it. Commit is the commit
// the catalog published for the entry: with its digest, the entry's
// version.
type catalogEntry struct {
	api.CatalogEntryView
	Commit string `json:"commit,omitempty"`
}

// catalogListView is the catalog with each entry's version.
type catalogListView struct {
	api.CatalogView
	Entries []catalogEntry `json:"entries"`
}

// version is what a person reads as the entry's version: its revision and
// the commit published, or just the revision when the catalog published no
// commit.
func (e catalogEntry) version() string {
	if e.Commit == "" {
		return e.Source.Revision
	}
	return e.Source.Revision + " @ " + short(e.Commit)
}

// needsOf is, in a few words, what an entry asks for at install.
func needsOf(e api.CatalogEntryView) string {
	var parts []string
	keys := 0
	for _, in := range e.Inputs {
		if in.Required {
			keys++
		}
	}
	if keys > 0 {
		parts = append(parts, fmt.Sprintf("%d input(s)", keys))
	}
	if len(e.Volumes) > 0 {
		parts = append(parts, "disk")
	}
	for _, r := range e.Resources {
		parts = append(parts, r.Kind)
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}

func newCatalogShowCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "show <entry>",
		Short: "Show an entry: what it is, what it asks at install",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			ac, err := newAppClient(g, g.progress(cmd))
			if err != nil {
				return err
			}
			raw, err := ac.serverRequest(ctx, "GET", "api/catalog/entries/"+url.PathEscape(args[0]), nil, "")
			if err != nil {
				return err
			}
			var e catalogEntry
			if err := json.Unmarshal(raw, &e); err != nil {
				return fmt.Errorf("decode entry: %w", err)
			}
			return g.print(cmd, e, func(w io.Writer) {
				fmt.Fprintf(w, "%s (%s)\n%s\n\n", e.DisplayName, e.Name, e.Description)
				tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
				fmt.Fprintf(tw, "Category\t%s\n", e.Category)
				if e.Publisher != "" {
					fmt.Fprintf(tw, "Publisher\t%s\n", e.Publisher)
				}
				if e.License != "" {
					fmt.Fprintf(tw, "License\t%s\n", e.License)
				}
				if e.Homepage != "" {
					fmt.Fprintf(tw, "Homepage\t%s\n", e.Homepage)
				}
				fmt.Fprintf(tw, "Source\t%s at %s\n", e.Source.Git, e.Source.Revision)
				if e.Commit != "" {
					fmt.Fprintf(tw, "Version\t%s (commit %s, digest %s)\n", e.Source.Revision, e.Commit, short(strings.TrimPrefix(e.Digest, "sha256:")))
				}
				fmt.Fprintf(tw, "Build\t%s\n", e.Build)
				if len(e.Overlay) > 0 {
					fmt.Fprintf(tw, "Overlay\t%s\n", strings.Join(e.Overlay, ", "))
				}
				fmt.Fprintf(tw, "Processes\t%s\n", strings.Join(e.Processes, ", "))
				for _, v := range e.Volumes {
					fmt.Fprintf(tw, "Volume\t%s, %s\n", v.Name, v.Size)
				}
				for _, r := range e.Resources {
					fmt.Fprintf(tw, "Resource\t%s %s\n", r.Kind, r.Name)
				}
				tw.Flush()
				asked := false
				for _, in := range e.Inputs {
					if in.Generate > 0 {
						continue
					}
					if !asked {
						fmt.Fprintln(w, "\nAsked at install (--input NAME=value):")
						asked = true
					}
					line := "  " + in.Name + ": " + in.Label
					switch {
					case in.Required:
						line += " (required)"
					case in.Default != "":
						line += " (default " + in.Default + ")"
					}
					if in.Secret {
						line += ", kept secret"
					}
					fmt.Fprintln(w, line)
					if in.Help != "" {
						fmt.Fprintf(w, "    %s\n", in.Help)
					}
				}
				if e.Readme != "" {
					fmt.Fprintf(w, "\n%s\n", strings.TrimSpace(e.Readme))
				}
			})
		},
	}
}

func newCatalogInstallCmd(g *globalFlags) *cobra.Command {
	var (
		slug     string
		name     string
		inputs   []string
		revision string
		access   string
		exposure string
	)
	cmd := &cobra.Command{
		Use:   "install <entry>",
		Short: "Make a project from an entry",
		Long: `Installs an entry as a project: the project, its disks and resources, the
values you give for what the entry asks (--input NAME=value, one per
input), then the code, which is the first deploy. The project is on the
version of the entry the catalog published, a fixed commit of the code, and
stays there until you apply an update (shpyrd catalog update). --revision
installs another tag or commit of the code instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			req := api.CatalogInstallRequest{Slug: slug, Name: name, Revision: revision, Access: access, Exposure: exposure, Inputs: map[string]string{}}
			for _, kv := range inputs {
				k, v, ok := strings.Cut(kv, "=")
				if !ok || k == "" {
					return fmt.Errorf("--input takes NAME=value, got %q", kv)
				}
				req.Inputs[k] = v
			}
			ac, err := newAppClient(g, g.progress(cmd))
			if err != nil {
				return err
			}
			body, _ := json.Marshal(req)
			raw, err := ac.serverRequest(ctx, "POST", "api/catalog/entries/"+url.PathEscape(args[0])+"/install", body, "application/json")
			if err != nil {
				return err
			}
			var resp api.CatalogInstallResponse
			if err := json.Unmarshal(raw, &resp); err != nil {
				return fmt.Errorf("decode install: %w", err)
			}
			return g.print(cmd, resp, func(w io.Writer) {
				p := resp.Project
				fmt.Fprintf(w, "Installed %s as project %s", args[0], p.Slug)
				if p.URL != "" {
					fmt.Fprintf(w, ", answering at %s once built", p.URL)
				}
				fmt.Fprintln(w, ".")
				for _, n := range resp.Notes {
					fmt.Fprintf(w, "Note: %s\n", n)
				}
				fmt.Fprintf(w, "Follow the first build with: shpyrd logs --project %s --build\n", p.Slug)
			})
		},
	}
	cmd.Flags().StringVar(&slug, "slug", "", "the project's slug (default: the entry's name)")
	cmd.Flags().StringVar(&name, "name", "", "the project's name (default: the entry's)")
	cmd.Flags().StringArrayVar(&inputs, "input", nil, "a value the entry asks for, NAME=value (repeatable)")
	cmd.Flags().StringVar(&revision, "revision", "", "install this tag or commit of the code instead of the version the catalog published")
	cmd.Flags().StringVar(&access, "access", "", "who may open the app: public, authenticated or identified")
	cmd.Flags().StringVar(&exposure, "exposure", "", "external (default) or internal")
	return cmd
}

func newCatalogStatusCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status <project>",
		Short: "Show the version of its catalog entry a project runs, and what an update would do",
		Long: `For a project made from the catalog: the version it runs, whether the catalog
offers another, and what applying it would do (inputs asked, volumes and
resources made, processes added or changed). It changes nothing.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			ac, err := newAppClient(g, g.progress(cmd))
			if err != nil {
				return err
			}
			raw, err := ac.serverRequest(ctx, "GET", "api/projects/"+url.PathEscape(args[0])+"/catalog/update", nil, "")
			if err != nil {
				return err
			}
			var p api.CatalogUpdatePreview
			if err := json.Unmarshal(raw, &p); err != nil {
				return fmt.Errorf("decode preview: %w", err)
			}
			return g.print(cmd, p, func(w io.Writer) { printCatalogPreview(w, args[0], p) })
		},
	}
}

// versionText is a version as a person reads it: "v1.4 @ 3f2a9c1b8d7e".
func versionText(v *api.CatalogVersionView) string {
	switch {
	case v == nil:
		return "unknown"
	case v.Revision == "":
		return v.ShortCommit
	}
	return v.Revision + " @ " + v.ShortCommit
}

func printCatalogPreview(w io.Writer, project string, p api.CatalogUpdatePreview) {
	fmt.Fprintf(w, "Project %s is made from the catalog entry %s.\nRuns:     %s\n", project, p.Entry, versionText(p.From))
	switch p.State {
	case api.CatalogUpToDate:
		fmt.Fprintln(w, "It is up to date.")
		return
	case api.CatalogUpdating:
		fmt.Fprintf(w, "Updating to %s: the update is rolling out.\n", versionText(p.To))
		return
	case api.CatalogUpdateAvailable:
	default:
		fmt.Fprintf(w, "No update is offered: %s.\n", p.Reason)
		return
	}
	fmt.Fprintf(w, "Offered:  %s\nCode:     %s\n", versionText(p.To), p.Repository)
	fmt.Fprintln(w, "\nApplying it:")
	fmt.Fprintln(w, "  sets the code to the offered commit and deploys it")
	if p.BuildChanged {
		fmt.Fprintln(w, "  changes the build settings")
	}
	if p.OverlayChanged {
		fmt.Fprintln(w, "  changes the files the entry lays over the code")
	}
	list := func(what string, items []string) {
		if len(items) > 0 {
			fmt.Fprintf(w, "  %s: %s\n", what, strings.Join(items, ", "))
		}
	}
	list("makes volumes", p.Volumes)
	list("makes resources", p.Resources)
	list("adds processes", p.ProcessesAdded)
	list("changes processes", p.ProcessesChanged)
	list("sets variables", p.EnvChanged)
	for _, in := range p.Inputs {
		switch in.Action {
		case "ask":
			need := "optional"
			if in.Required {
				need = "required"
			}
			fmt.Fprintf(w, "  asks for %s (%s): --input %s=value\n", in.Name, need, in.Name)
		case "generate":
			fmt.Fprintf(w, "  generates %s\n", in.Name)
		case "restore":
			fmt.Fprintf(w, "  takes back %s, the value the project had\n", in.Name)
		default:
			if in.Default != "" {
				fmt.Fprintf(w, "  sets %s to its default, %s\n", in.Name, in.Default)
			} else {
				fmt.Fprintf(w, "  sets %s to its default\n", in.Name)
			}
		}
	}
	fmt.Fprintln(w, "\nIt keeps every config var, replica count and size you set, and removes nothing.")
}

func newCatalogUpdateCmd(g *globalFlags) *cobra.Command {
	var (
		inputs []string
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "update <project>",
		Short: "Update a project to the version of its entry the catalog offers",
		Long: `Shows what the update does, asks for confirmation (--yes skips it), then
applies it: the code moves to the offered commit and is deployed. An update
only adds: it keeps every config var, replica count and size you set, and
removes nothing. Inputs the entry adds are given with --input NAME=value;
see them with 'shpyrd catalog status <project>'.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := signalContext()
			req := api.CatalogUpdateRequest{Inputs: map[string]string{}}
			for _, kv := range inputs {
				k, v, ok := strings.Cut(kv, "=")
				if !ok || k == "" {
					return fmt.Errorf("--input takes NAME=value, got %q", kv)
				}
				req.Inputs[k] = v
			}
			ac, err := newAppClient(g, g.progress(cmd))
			if err != nil {
				return err
			}
			path := "api/projects/" + url.PathEscape(args[0]) + "/catalog/update"
			raw, err := ac.serverRequest(ctx, "GET", path, nil, "")
			if err != nil {
				return err
			}
			var p api.CatalogUpdatePreview
			if err := json.Unmarshal(raw, &p); err != nil {
				return fmt.Errorf("decode preview: %w", err)
			}
			if p.State != api.CatalogUpdateAvailable {
				// Nothing to apply: say why, and change nothing.
				return g.print(cmd, api.CatalogUpdateResult{Reason: p.Reason, From: p.From, To: p.To}, func(w io.Writer) { printCatalogPreview(w, args[0], p) })
			}
			printCatalogPreview(g.progress(cmd), args[0], p)
			if !confirm(cmd, yes, fmt.Sprintf("Update %s to %s?", args[0], versionText(p.To)), false) {
				return fmt.Errorf("aborted")
			}
			req.Digest = p.To.Digest
			body, _ := json.Marshal(req)
			raw, err = ac.serverRequest(ctx, "POST", path, body, "application/json")
			if err != nil {
				return err
			}
			var res api.CatalogUpdateResult
			if err := json.Unmarshal(raw, &res); err != nil {
				return fmt.Errorf("decode update: %w", err)
			}
			return g.print(cmd, res, func(w io.Writer) {
				if !res.Changed {
					fmt.Fprintf(w, "Nothing was changed: %s.\n", res.Reason)
					return
				}
				fmt.Fprintf(w, "Updating %s to %s.\n", args[0], versionText(res.To))
				for _, n := range res.Notes {
					fmt.Fprintf(w, "Note: %s\n", n)
				}
				fmt.Fprintf(w, "Follow the build with: shpyrd logs --project %s --build\n", args[0])
			})
		},
	}
	cmd.Flags().StringArrayVar(&inputs, "input", nil, "a value for an input the update adds, NAME=value (repeatable)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}

func newCatalogValidateCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "validate <dir>",
		Short: "Check the entries of a catalog repository (a folder per entry)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			names, problems, err := validateEntries(args[0])
			if err != nil {
				return err
			}
			type report struct {
				Entries  []string `json:"entries"`
				Problems []string `json:"problems,omitempty"`
			}
			out := report{Entries: names, Problems: problems}
			if err := g.print(cmd, out, func(w io.Writer) {
				for _, n := range names {
					fmt.Fprintf(w, "%s: ok\n", n)
				}
				for _, p := range problems {
					fmt.Fprintln(w, p)
				}
			}); err != nil {
				return err
			}
			if len(problems) > 0 {
				return fmt.Errorf("%d entr%s with problems", len(problems), map[bool]string{true: "y", false: "ies"}[len(problems) == 1])
			}
			return nil
		},
	}
}

// validateEntries reads every entry folder under dir: the names of the
// good ones, and a line per bad one.
func validateEntries(dir string) (names, problems []string, err error) {
	folders, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	for _, f := range folders {
		if !f.IsDir() || strings.HasPrefix(f.Name(), ".") {
			continue
		}
		if _, err := catalog.ReadEntry(filepath.Join(dir, f.Name())); err != nil {
			var ve *catalog.ValidationError
			if errors.As(err, &ve) {
				problems = append(problems, fmt.Sprintf("%s:\n  - %s", ve.Name, strings.Join(ve.Problems, "\n  - ")))
			} else {
				problems = append(problems, err.Error())
			}
			continue
		}
		names = append(names, f.Name())
	}
	sort.Strings(names)
	if len(names)+len(problems) == 0 {
		return nil, nil, fmt.Errorf("%s holds no entry: an entry is a folder with a %s", dir, catalog.EntryFile)
	}
	return names, problems, nil
}

func newCatalogPublishCmd(g *globalFlags) *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "publish <dir>",
		Short: "Write the index a catalog repository serves (its CI runs this)",
		Long: `Reads the entry folders under <dir> and writes to --out what a web server
publishes as the catalog: index.json, and under entries/<name>/ the readme,
the icon and the overlay archive of each entry. The server reads the index
by its address (SHPYRD_CATALOG_URL).

A version of an entry is its folder together with the commit its revision
names when you publish. Publishing looks each entry's repository up and
writes that commit in the index; a revision that cannot be found stops the
publish, and nothing is written. A revision that is a full commit needs no
lookup. A branch is accepted, but the version is then the branch's head at
the moment you publish: pin a tag or a commit for a version that does not
depend on when the catalog was published.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return publishCatalog(cmd, g, args[0], out, time.Now(), catalogRefs)
		},
	}
	cmd.Flags().StringVar(&out, "out", "public", "the folder to write")
	return cmd
}

// catalogRefs lists a public repository's branches and tags over the
// network; tests of the command replace it.
var catalogRefs catalog.RefLister = lsRemoteRefs

func lsRemoteRefs(ctx context.Context, url string) (map[string]string, error) {
	return gitauth.LsRemote(ctx, url, gitauth.Build{})
}

// publishCatalog is `catalog publish`, with the repository listing given
// so that tests need no network.
func publishCatalog(cmd *cobra.Command, g *globalFlags, dir, out string, now time.Time, list catalog.RefLister) error {
	ix, resolved, err := catalog.PublishResolved(cmd.Context(), dir, out, now, list)
	if err != nil {
		return err
	}
	byName := map[string]catalog.Resolved{}
	for _, r := range resolved {
		byName[r.Name] = r
	}
	return g.print(cmd, ix, func(w io.Writer) {
		fmt.Fprintf(w, "Published %d entries to %s\n", len(ix.Entries), filepath.Join(out, catalog.IndexFile))
		branches := 0
		for _, e := range ix.Entries {
			line := "  " + e.Name
			if e.Overlay != nil {
				line += " (overlay: " + strings.Join(e.Overlay.Files, ", ") + ")"
			}
			if r, ok := byName[e.Name]; ok {
				line += ": " + r.Revision + " at " + shortCommit(r.Commit)
				if r.Branch {
					line += " (BRANCH)"
					branches++
				}
			}
			fmt.Fprintln(w, line)
		}
		if branches > 0 {
			noun := "entries name a branch"
			if branches == 1 {
				noun = "entry names a branch"
			}
			fmt.Fprintf(w, "\nWarning: %d %s. Its version is the branch's head as of this publish, so it\nchanges only when the catalog is published again. Pin a tag or a commit to make the version\nindependent of when you publish.\n", branches, noun)
		}
	})
}

// shortCommit is the first 12 characters of a commit name.
func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}
