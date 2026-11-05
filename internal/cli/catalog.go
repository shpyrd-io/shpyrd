package cli

import (
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
with all of that, which then follows the upstream branch as any git project
does.

  shpyrd catalog list
  shpyrd catalog show fluxyr
  shpyrd catalog install fluxyr --input OPENROUTER_API_KEY=sk-or-...

The catalog is a repository of entry folders, published as an index the
server reads (SHPYRD_CATALOG_URL). 'validate' checks a folder of entries,
and 'publish' writes the index its CI serves.`,
		Args: cobra.NoArgs,
		RunE: catalogList(g),
	}
	cmd.AddCommand(newCatalogListCmd(g), newCatalogShowCmd(g), newCatalogInstallCmd(g), newCatalogValidateCmd(g), newCatalogPublishCmd(g))
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
		var view api.CatalogView
		if err := json.Unmarshal(raw, &view); err != nil {
			return fmt.Errorf("decode catalog: %w", err)
		}
		return g.print(cmd, view, func(w io.Writer) {
			if len(view.Entries) == 0 {
				fmt.Fprintln(w, "The catalog is empty.")
				return
			}
			tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ENTRY\tNAME\tCATEGORY\tNEEDS\tDESCRIPTION")
			for _, e := range view.Entries {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", e.Name, e.DisplayName, e.Category, needsOf(e), e.Description)
			}
			tw.Flush()
			fmt.Fprintf(w, "\nFrom %s. Install one with: shpyrd catalog install <entry>\n", view.URL)
		})
	}
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
			var e api.CatalogEntryView
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
input), then the code, which is the first deploy. The project follows the
entry's branch from then on; --revision pins a tag or a commit instead.`,
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
	cmd.Flags().StringVar(&revision, "revision", "", "pin a tag or commit instead of following the entry's branch")
	cmd.Flags().StringVar(&access, "access", "", "who may open the app: public, authenticated or identified")
	cmd.Flags().StringVar(&exposure, "exposure", "", "external (default) or internal")
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
by its address (SHPYRD_CATALOG_URL).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ix, err := catalog.Publish(args[0], out, time.Now())
			if err != nil {
				return err
			}
			return g.print(cmd, ix, func(w io.Writer) {
				fmt.Fprintf(w, "Published %d entries to %s\n", len(ix.Entries), filepath.Join(out, catalog.IndexFile))
				for _, e := range ix.Entries {
					line := "  " + e.Name
					if e.Overlay != nil {
						line += " (overlay: " + strings.Join(e.Overlay.Files, ", ") + ")"
					}
					fmt.Fprintln(w, line)
				}
			})
		},
	}
	cmd.Flags().StringVar(&out, "out", "public", "the folder to write")
	return cmd
}
