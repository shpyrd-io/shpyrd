package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The console's users: who may open the operator's console, by email,
// independent of every workspace (all admins for now). They sign in with
// an auth-local account, or later through the console's SSO; the list is
// what lets them in.

// ConsoleUserView is a console user with the state of their account.
type ConsoleUserView struct {
	store.ConsoleUser
	// Account is the auth-local account's state: active, pending, locked,
	// or "" when there is none (an SSO sign-in still works).
	Account string `json:"account"`
}

// AddConsoleUserRequest puts someone on the list. With a password and no
// account yet, the account is made with it.
type AddConsoleUserRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password,omitempty"`
}

func (s *Server) accountOf(c *gin.Context, email string) string {
	if s.localAccounts == nil {
		return ""
	}
	st, err := s.localAccounts.Status(c.Request.Context(), email)
	if err != nil {
		return ""
	}
	return st
}

func (s *Server) listConsoleUsers(c *gin.Context) {
	list, err := s.store.ListConsoleUsers(c.Request.Context())
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	out := make([]ConsoleUserView, 0, len(list))
	for _, u := range list {
		out = append(out, ConsoleUserView{ConsoleUser: u, Account: s.accountOf(c, u.Email)})
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) addConsoleUser(c *gin.Context) {
	var req AddConsoleUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !strings.Contains(email, "@") {
		abort(c, http.StatusBadRequest, errors.New("a console user is an email"))
		return
	}
	ctx := c.Request.Context()
	if req.Password != "" {
		if s.localAccounts == nil {
			abort(c, http.StatusBadRequest, errors.New("accounts with a password need the auth-local extension"))
			return
		}
		if st, err := s.localAccounts.Status(ctx, email); err == nil && st != "" {
			abort(c, http.StatusConflict, errors.New(email+" already has an account: leave the password out, or change it under Accounts"))
			return
		}
		if err := s.localAccounts.CreatePending(ctx, email, ""); err != nil {
			abort(c, http.StatusBadRequest, err)
			return
		}
		if err := s.localAccounts.SetPasswordAndVerify(ctx, email, req.Password); err != nil {
			abort(c, http.StatusBadRequest, err)
			return
		}
	}
	id, _ := ext.IdentityFrom(c)
	// The first console user ends bootstrap mode: a person adding someone
	// else then is put on the list too, or the change would take their own
	// access with it (claimOwnershipInBootstrap does the same for owners).
	if roles, err := s.rolesOf(c); err == nil && !roles.Enforced && id.Email != "" && id.Provider != "token" && id.Provider != "kubeconfig" {
		if _, err := s.store.AddConsoleUser(ctx, id.Email, id.Email); err != nil {
			abort(c, http.StatusBadGateway, err)
			return
		}
	}
	u, err := s.store.AddConsoleUser(ctx, email, firstNonEmpty(id.Email, id.Subject))
	if err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	s.authz.Invalidate()
	s.audit(c, "", "console-user.add", email, "")
	c.JSON(http.StatusCreated, ConsoleUserView{ConsoleUser: *u, Account: s.accountOf(c, email)})
}

func (s *Server) removeConsoleUser(c *gin.Context) {
	email := strings.ToLower(strings.TrimSpace(c.Param("email")))
	if id, ok := ext.IdentityFrom(c); ok && strings.EqualFold(id.Email, email) {
		abort(c, http.StatusBadRequest, errors.New("you cannot take yourself off the console"))
		return
	}
	if err := s.store.RemoveConsoleUser(c.Request.Context(), email); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			abort(c, http.StatusNotFound, errors.New(email+" is not a console user"))
			return
		}
		abort(c, http.StatusBadGateway, err)
		return
	}
	s.authz.Invalidate()
	s.audit(c, "", "console-user.remove", email, "")
	c.Status(http.StatusNoContent)
}
