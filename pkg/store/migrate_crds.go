package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// Teams and project members were Kubernetes objects before RFC-0033.
// ImportCRDs copies each one into the store on start and marks it
// (shpyrdv1.AnnotationMigrated); the installer removes the definitions once
// every object is marked (#249). It goes object by object: an earlier
// version imported only into a store with no teams at all, which the
// built-in team "everyone" made impossible, so a cluster upgraded since then
// may hold objects that were never imported. Idempotent: a marked object is
// skipped, and a run cut short is finished by the next start.

// LegacyImport is what ImportCRDs did.
type LegacyImport struct {
	Teams, Grants int
	// Kept names teams the store already had under the same name: the
	// store's version wins, since it may have been edited since.
	Kept []string
	// Skipped names objects that could not be imported, with why: a grant
	// to a team or project that is gone.
	Skipped []string
}

// ImportCRDs imports every unmarked legacy Team and ProjectMember into
// workspace ws. Definitions that are absent mean nothing to import.
func ImportCRDs(ctx context.Context, dyn dynamic.Interface, st Store, ws string) (LegacyImport, error) {
	var out LegacyImport
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	teams, err := listLegacy(ctx, dyn, shpyrdv1.LegacyTeams)
	if err != nil {
		return out, err
	}
	for i := range teams {
		u := &teams[i]
		if u.GetAnnotations()[shpyrdv1.AnnotationMigrated] != "" {
			continue
		}
		name := u.GetName()
		mark := stamp
		switch _, err := st.GetTeam(ctx, ws, name); {
		case err == nil:
			out.Kept = append(out.Kept, name)
		case errors.Is(err, ErrNotFound):
			members, _, _ := unstructured.NestedStringSlice(u.Object, "spec", "members")
			groups, _, _ := unstructured.NestedStringSlice(u.Object, "spec", "groups")
			desc, _, _ := unstructured.NestedString(u.Object, "spec", "description")
			role, _, _ := unstructured.NestedString(u.Object, "spec", "platformRole")
			// Members by email, as an old backup carries them: restored
			// the same way (ensureContacts).
			if err := ensureContacts(ctx, st, ws, members); err != nil {
				return out, fmt.Errorf("team %s: %w", name, err)
			}
			_, _, err := st.PutTeam(ctx, ws, Team{Name: name, Description: desc, Members: members, Groups: groups, PlatformRole: role})
			switch {
			case err == nil:
				out.Teams++
			case errors.Is(err, ErrBuiltIn):
				mark = "skipped: " + name + " is a built-in team"
				out.Skipped = append(out.Skipped, "team "+name+": a built-in team has that name")
			default:
				return out, fmt.Errorf("team %s: %w", name, err)
			}
		default:
			return out, fmt.Errorf("team %s: %w", name, err)
		}
		if err := markLegacy(ctx, dyn, shpyrdv1.LegacyTeams, u, mark); err != nil {
			return out, err
		}
	}
	members, err := listLegacy(ctx, dyn, shpyrdv1.LegacyProjectMembers)
	if err != nil {
		return out, err
	}
	for i := range members {
		u := &members[i]
		if u.GetAnnotations()[shpyrdv1.AnnotationMigrated] != "" {
			continue
		}
		project, _, _ := unstructured.NestedString(u.Object, "spec", "project")
		role, _, _ := unstructured.NestedString(u.Object, "spec", "role")
		user, _, _ := unstructured.NestedString(u.Object, "spec", "user")
		team, _, _ := unstructured.NestedString(u.Object, "spec", "team")
		mark := stamp
		if user != "" {
			if err := ensureContacts(ctx, st, ws, []string{user}); err != nil {
				return out, fmt.Errorf("project member %s: %w", u.GetName(), err)
			}
		}
		_, err := st.AddGrant(ctx, ws, Grant{Project: project, Role: role, User: user, Team: team})
		switch {
		case err == nil:
			out.Grants++
		case errors.Is(err, ErrConflict):
			// the store has it already
		case errors.Is(err, ErrNotFound):
			mark = "skipped: its team or project is gone"
			out.Skipped = append(out.Skipped, fmt.Sprintf("project member %s (%s on %s for %s): its team or project is gone", u.GetName(), role, project, firstOf(user, "team "+team)))
		default:
			return out, fmt.Errorf("project member %s: %w", u.GetName(), err)
		}
		if err := markLegacy(ctx, dyn, shpyrdv1.LegacyProjectMembers, u, mark); err != nil {
			return out, err
		}
	}
	return out, nil
}

// listLegacy lists a legacy resource; none when its definition is gone.
func listLegacy(ctx context.Context, dyn dynamic.Interface, gvr schema.GroupVersionResource) ([]unstructured.Unstructured, error) {
	list, err := dyn.Resource(gvr).List(ctx, metav1.ListOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", gvr.Resource, err)
	}
	return list.Items, nil
}

// markLegacy records on the object that it was dealt with. A failure stops
// the import: the object would otherwise look unimported to the installer
// forever, and the next start retries it harmlessly.
func markLegacy(ctx context.Context, dyn dynamic.Interface, gvr schema.GroupVersionResource, u *unstructured.Unstructured, value string) error {
	ann := u.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	ann[shpyrdv1.AnnotationMigrated] = value
	u.SetAnnotations(ann)
	if _, err := dyn.Resource(gvr).Update(ctx, u, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("mark %s %s as migrated: %w", gvr.Resource, u.GetName(), err)
	}
	return nil
}

func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
