package mail

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/shpyrd-io/shpyrd/pkg/audit"
	"github.com/shpyrd-io/shpyrd/pkg/emails"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

var hostnameFn = os.Hostname

type extension struct{}

// New returns the extension.
func New() ext.Extension { return extension{} }

func (extension) Name() string { return Name }
func (extension) Description() string {
	return "Send email from the platform: invitations and notifications over SMTP (shpyrd-ctl mail set)"
}

// Components is nil: the extension is configuration held in a Secret.
func (extension) Components() []ext.ComponentRef        { return nil }
func (extension) Register(ctrl.Manager, ext.Deps) error { return nil }
func (extension) Types() []ext.ResourceType             { return nil }
func (extension) CLI(g ext.CLIGlobals) []*cobra.Command {
	return []*cobra.Command{ext.ForOperator(newMailCmd(g))}
}

// Mailer implements ext.MailProvider: the server hands the Sender to
// whoever sends mail (invitations, other extensions).
func (extension) Mailer(deps ext.Deps) ext.Mailer {
	if deps.Kube == nil || deps.Kube.Kube == nil {
		return nil
	}
	return NewSender(&Store{Kube: deps.Kube.Kube, Namespace: deps.SystemNamespace})
}

// Routes: the Cluster page's Email card reads the status and sends a test
// message (console, platform admins).
func (e extension) Routes(r ext.Router, deps ext.Deps) error {
	sender, _ := deps.Mail.(*Sender)
	if sender == nil {
		if m := e.Mailer(deps); m != nil {
			sender = m.(*Sender)
		}
	}
	if sender == nil {
		return nil
	}
	h := &handlers{sender: sender, deps: deps}
	admin := r.Admin()
	admin.GET("/cluster/mail", h.status)
	admin.POST("/cluster/mail/test", h.test)
	return nil
}

type handlers struct {
	sender *Sender
	deps   ext.Deps
}

func (h *handlers) status(c *gin.Context) {
	st, err := h.sender.Status(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, st)
}

// TestRequest is POST /api/cluster/mail/test.
type TestRequest struct {
	To string `json:"to" binding:"required"`
}

func (h *handlers) test(c *gin.Context) {
	var req TestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "to is required"})
		return
	}
	to, err := mail.ParseAddress(strings.TrimSpace(req.To))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "that is not an email address"})
		return
	}
	ctx := c.Request.Context()
	if !h.sender.Configured(ctx) {
		c.JSON(http.StatusConflict, gin.H{"error": ErrNotConfigured.Error()})
		return
	}
	st, _ := h.sender.Status(ctx)
	sent := time.Now().UTC().Format(time.RFC1123)
	door := doorOf(c)
	subject, body, err := emails.Render(emails.Test, emails.Words{
		"Logo": emails.Logo(door), "Door": emails.Host(door), "Host": st.Host, "Port": strconv.Itoa(st.Port),
		"Security": st.Security, "From": st.From, "Sent": sent,
	})
	if err != nil {
		subject = "shpyrd test message"
	}
	msg := ext.Message{
		To:      []string{to.Address},
		Subject: subject,
		Text: fmt.Sprintf("This is a test message from your shpyrd platform.\n\nSent through %s:%d (%s) as %s at %s.\n\nIf you received it, email delivery works.\n",
			st.Host, st.Port, st.Security, st.From, sent),
		HTML: body,
	}
	start := time.Now()
	err = h.sender.Send(ctx, msg)
	took := time.Since(start).Round(time.Millisecond)
	if err != nil {
		h.audit(c, "mail.test_failed", to.Address, err.Error())
		status := http.StatusBadGateway
		if errors.Is(err, ErrRateLimited) {
			status = http.StatusTooManyRequests
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	h.audit(c, "mail.test", to.Address, "delivered in "+took.String())
	c.JSON(http.StatusOK, gin.H{"ok": true, "to": to.Address, "took": took.String()})
}

func (h *handlers) audit(c *gin.Context, action, target, detail string) {
	if h.deps.Kube == nil || h.deps.Kube.Kube == nil {
		return
	}
	id, _ := ext.IdentityFrom(c)
	actor := id.Email
	if actor == "" {
		actor = id.Name
	}
	if actor == "" {
		actor = id.Subject
	}
	entry := audit.Entry{Actor: actor, Action: action, Target: target, Detail: detail, From: c.ClientIP(), Via: "api"}
	_ = audit.Record(context.WithoutCancel(c.Request.Context()), h.deps.Kube.Kube, audit.ClusterRef(h.deps.SystemNamespace), entry)
}

// doorOf is the address the request came in on (https://acme.shpyrd.app),
// where the email's mark is served.
func doorOf(c *gin.Context) string {
	scheme := "https"
	if c.Request.TLS == nil && c.GetHeader("X-Forwarded-Proto") == "http" {
		scheme = "http"
	}
	return scheme + "://" + c.Request.Host
}
