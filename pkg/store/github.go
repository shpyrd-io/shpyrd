package store

import (
	"context"
	"errors"
	"time"
)

// ErrGitHubInstallationHeld is an installation another workspace has: it
// is never taken over. The error is a *GitHubHeldError, which names it.
var ErrGitHubInstallationHeld = errors.New("the GitHub installation is connected to another workspace")

// GitHubHeldError says which workspace has the installation.
type GitHubHeldError struct {
	// Workspace is the slug of the workspace that has it.
	Workspace string
}

func (e *GitHubHeldError) Error() string {
	return "the GitHub installation is connected to workspace " + e.Workspace
}

func (e *GitHubHeldError) Is(target error) bool { return target == ErrGitHubInstallationHeld }

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

// GitHubUserLink is a person's GitHub account, linked through the GitHub
// App's user authorization (RFC-0054): one per shpyrd identity, its
// provider and subject. RefreshToken is sealed (pkg/tokenseal); the access
// token is never stored.
type GitHubUserLink struct {
	Provider         string    `json:"provider"`
	Subject          string    `json:"subject"`
	GitHubUserID     int64     `json:"githubUserId"`
	GitHubLogin      string    `json:"githubLogin"`
	RefreshToken     string    `json:"-"`
	RefreshExpiresAt time.Time `json:"refreshExpiresAt"`
	LinkedAt         time.Time `json:"linkedAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// GitHub is the GitHub App part of the Store.
type GitHub interface {
	// AddGitHubInstallation connects an installation to a workspace (slug
	// or ID), by GitHub's id: a new one, one connected to this workspace
	// before, or one disconnected or uninstalled since. One another
	// workspace has is refused with a *GitHubHeldError
	// (ErrGitHubInstallationHeld), checked in the same transaction.
	AddGitHubInstallation(ctx context.Context, ws string, inst GitHubInstallation) error
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
	// PutGitHubUserLink links an identity, or links it again: the refresh
	// token rotated, the login renamed, or another GitHub account.
	// LinkedAt stays while the GitHub account (its id) is the same.
	PutGitHubUserLink(ctx context.Context, l GitHubUserLink) error
	// GitHubUserLink is the identity's link; ErrNotFound when it has none.
	GitHubUserLink(ctx context.Context, provider, subject string) (*GitHubUserLink, error)
	// DeleteGitHubUserLink unlinks; ErrNotFound when there is no link.
	DeleteGitHubUserLink(ctx context.Context, provider, subject string) error
	// ReplaceGitHubUserLinkToken is the refresh's write: it replaces the
	// token, login, user id and expiry of l only if the stored refresh token
	// (the sealed value, as stored) still equals oldSealed. It returns false,
	// without error, when it changed or the row is gone: another replica
	// rotated it, so a stale refresher never overwrites. LinkedAt stays as
	// in PutGitHubUserLink; UpdatedAt moves.
	ReplaceGitHubUserLinkToken(ctx context.Context, provider, subject, oldSealed string, l GitHubUserLink) (bool, error)
	// DeleteGitHubUserLinkIf unlinks only if the stored refresh token (the
	// sealed value, as stored) still equals sealed; false, without error,
	// when it changed or the row is gone.
	DeleteGitHubUserLinkIf(ctx context.Context, provider, subject, sealed string) (bool, error)
}
