package store

import (
	"context"
	"time"
)

// GitHubInstallation is a GitHub App installation connected to a
// workspace (RFC-0054): GitHub's id, the account it is installed on, who
// connected it, and its state on GitHub.
type GitHubInstallation struct {
	ID int64 `json:"id"`
	// Workspace is the slug of the workspace it is connected to.
	Workspace    string `json:"workspace"`
	AccountLogin string `json:"accountLogin"`
	// AccountType is GitHub's: Organization or User.
	AccountType string     `json:"accountType"`
	ConnectedBy string     `json:"connectedBy"`
	CreatedAt   time.Time  `json:"createdAt"`
	SuspendedAt *time.Time `json:"suspendedAt,omitempty"`
	DeletedAt   *time.Time `json:"deletedAt,omitempty"`
}

// GitHub is the GitHub App part of the Store.
type GitHub interface {
	// AddGitHubInstallation connects an installation to a workspace (slug
	// or ID), by GitHub's id: a new one, or one connected before, to this
	// workspace or another, or uninstalled since, taken over. It answers
	// the slug of the workspace it was connected to before, "" when none
	// or the same.
	AddGitHubInstallation(ctx context.Context, ws string, inst GitHubInstallation) (moved string, err error)
	// GitHubInstallations are a workspace's, not uninstalled, oldest first.
	GitHubInstallations(ctx context.Context, ws string) ([]GitHubInstallation, error)
	// GitHubInstallation finds one by id across workspaces; ErrNotFound
	// when no workspace has it or it was uninstalled.
	GitHubInstallation(ctx context.Context, id int64) (*GitHubInstallation, error)
	// SetGitHubInstallationState records what GitHub said: suspended or
	// not (the time is kept while it stays suspended), uninstalled.
	// ErrNotFound when there is no such row.
	SetGitHubInstallationState(ctx context.Context, id int64, suspended, deleted bool) error
	// DeleteGitHubInstallation disconnects one from a workspace;
	// ErrNotFound when the workspace does not have it.
	DeleteGitHubInstallation(ctx context.Context, ws string, id int64) error
}
