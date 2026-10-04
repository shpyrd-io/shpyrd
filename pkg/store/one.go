package store

import (
	"context"
	"errors"
	"sync"
)

// ErrOneWorkspace refuses a second workspace on the open-source platform.
var ErrOneWorkspace = errors.New("this platform runs one workspace")

// OneWorkspace is a store that shows one workspace, the default one: the
// open-source platform's. A workspace added to the database by hand is not
// there for the server or the controllers, and none can be created; the
// cloud, which runs many, uses the store as it is.
func OneWorkspace(st Store) Store { return &oneWorkspace{Store: st} }

type oneWorkspace struct {
	Store
	mu   sync.Mutex
	slug string
}

// defaultSlug is read once: nothing changes it on a running platform.
func (o *oneWorkspace) defaultSlug(ctx context.Context) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.slug == "" {
		if v, err := o.Store.GetSetting(ctx, SettingDefaultWorkspaceID); err == nil && v != "" {
			o.slug = v
		} else if err == nil || errors.Is(err, ErrNotFound) {
			o.slug = DefaultWorkspace
		} else {
			return DefaultWorkspace // unreachable now: ask again next time
		}
	}
	return o.slug
}

func (o *oneWorkspace) only(ctx context.Context, ws *Workspace, err error) (*Workspace, error) {
	if err != nil {
		return nil, err
	}
	if ws.Slug != o.defaultSlug(ctx) {
		return nil, ErrNotFound
	}
	return ws, nil
}

func (o *oneWorkspace) Workspace(ctx context.Context, slug string) (*Workspace, error) {
	if slug != o.defaultSlug(ctx) {
		return nil, ErrNotFound
	}
	return o.Store.Workspace(ctx, slug)
}

func (o *oneWorkspace) WorkspaceByAddress(ctx context.Context, address string) (*Workspace, error) {
	ws, err := o.Store.WorkspaceByAddress(ctx, address)
	return o.only(ctx, ws, err)
}

func (o *oneWorkspace) WorkspaceByHost(ctx context.Context, host string) (*Workspace, *WorkspaceHost, error) {
	ws, h, err := o.Store.WorkspaceByHost(ctx, host)
	if ws, err = o.only(ctx, ws, err); err != nil {
		return nil, nil, err
	}
	return ws, h, nil
}

func (o *oneWorkspace) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	all, err := o.Store.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	slug := o.defaultSlug(ctx)
	for i := range all {
		if all[i].Slug == slug {
			return []Workspace{all[i]}, nil
		}
	}
	return []Workspace{}, nil
}

func (o *oneWorkspace) CreateWorkspace(context.Context, Workspace) (*Workspace, error) {
	return nil, ErrOneWorkspace
}
