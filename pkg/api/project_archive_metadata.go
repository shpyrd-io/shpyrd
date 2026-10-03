package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/projectarchive"
	"github.com/shpyrd-io/shpyrd/pkg/registry"
)

type archivedVolume struct {
	Name string              `json:"name"`
	Spec shpyrdv1.VolumeSpec `json:"spec"`
}
type archivedDatabase struct {
	Name string                `json:"name"`
	Spec shpyrdv1.PostgresSpec `json:"spec"`
}
type projectArchiveMetadata struct {
	CreatedAt     time.Time                     `json:"createdAt"`
	Name          string                        `json:"name"`
	Spec          shpyrdv1.AppSpec              `json:"spec"`
	Status        shpyrdv1.AppStatus            `json:"status"`
	Config        map[string][]byte             `json:"config"`
	Globals       map[string][]byte             `json:"globals,omitempty"`
	ReleaseConfig map[int]map[string][]byte     `json:"releaseConfig,omitempty"`
	Volumes       []archivedVolume              `json:"volumes"`
	Databases     []archivedDatabase            `json:"databases"`
	Images        map[string]ocispec.Descriptor `json:"images,omitempty"`
	SourceEntry   string                        `json:"sourceEntry,omitempty"`
}

func (s *Server) projectArchiveMetadata(ctx context.Context, app *shpyrdv1.App) (*projectArchiveMetadata, error) {
	m := &projectArchiveMetadata{CreatedAt: time.Now().UTC(), Name: project.SlugOf(app), Spec: *app.Spec.DeepCopy(), Status: *app.Status.DeepCopy(), Config: map[string][]byte{}, Images: map[string]ocispec.Descriptor{}}
	secret := &corev1.Secret{}
	if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: app.EnvSecretName()}, secret); err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	} else if err == nil {
		m.Config = secret.Data
	}
	m.ReleaseConfig = map[int]map[string][]byte{}
	for _, release := range app.Status.Releases {
		snapshot := &corev1.Secret{}
		if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: app.ReleaseSnapshotName(release.Number)}, snapshot); err != nil && !apierrors.IsNotFound(err) {
			return nil, err
		} else if err == nil {
			m.ReleaseConfig[release.Number] = snapshot.Data
		}
	}
	global := &corev1.Secret{}
	if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: shpyrdv1.GlobalEnvSecretName}, global); err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	} else if err == nil {
		m.Globals = global.Data
	}
	var volumes shpyrdv1.VolumeList
	if err := s.apps.List(ctx, &volumes, client.InNamespace(app.Namespace)); err != nil {
		return nil, err
	}
	for _, v := range volumes.Items {
		m.Volumes = append(m.Volumes, archivedVolume{Name: v.Name, Spec: *v.Spec.DeepCopy()})
	}
	var databases shpyrdv1.PostgresList
	if err := s.apps.List(ctx, &databases, client.InNamespace(app.Namespace)); err != nil {
		return nil, err
	}
	for _, pg := range databases.Items {
		m.Databases = append(m.Databases, archivedDatabase{Name: pg.Name, Spec: *pg.Spec.DeepCopy()})
	}
	// Refuse an incomplete export rather than implying that unsupported
	// persistent resource contents are inside this archive.
	var redis shpyrdv1.RedisList
	if err := s.apps.List(ctx, &redis, client.InNamespace(app.Namespace)); err != nil {
		return nil, err
	}
	if len(redis.Items) > 0 {
		return nil, errors.New("portable archives currently support Postgres and volumes; this project also has Redis resources")
	}
	var buckets shpyrdv1.ObjectBucketList
	if err := s.apps.List(ctx, &buckets, client.InNamespace(app.Namespace)); err != nil {
		return nil, err
	}
	if len(buckets.Items) > 0 {
		return nil, errors.New("portable archives currently support Postgres and volumes; object bucket contents require a separate export")
	}
	sort.Slice(m.Volumes, func(i, j int) bool { return m.Volumes[i].Name < m.Volumes[j].Name })
	sort.Slice(m.Databases, func(i, j int) bool { return m.Databases[i].Name < m.Databases[j].Name })
	return m, nil
}

func (s *Server) archiveRepository(ctx context.Context, reference string) (*remote.Repository, string, error) {
	host, repo, digest := registry.SplitReference(reference)
	if host == "" || repo == "" || digest == "" {
		return nil, "", errors.New("portable export requires an immutable deployed image digest")
	}
	var credentials []byte
	if name := s.vars(install.VarRegistrySecret); name != "" {
		secret, err := s.kube.Kube.CoreV1().Secrets(s.kube.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, "", err
		}
		credentials = secret.Data[corev1.DockerConfigJsonKey]
	}
	credential := registry.FromDockerConfig(host, credentials)
	r, err := remote.NewRepository(host + "/" + repo)
	if err != nil {
		return nil, "", err
	}
	r.PlainHTTP = host == s.vars(install.VarRegistryHost) && s.vars(install.VarRegistryInsecure) == "true"
	r.Client = &auth.Client{Client: &http.Client{}, Cache: auth.NewCache(), Credential: auth.StaticCredential(host, auth.Credential{Username: credential.Username, Password: credential.Password})}
	return r, digest, nil
}

func (s *Server) exportProjectImages(ctx context.Context, b *projectarchive.Bundle, m *projectArchiveMetadata) error {
	refs := map[string]bool{}
	if m.Status.Image != "" {
		refs[m.Status.Image] = true
	}
	for _, r := range m.Status.Releases {
		if r.Image != "" {
			refs[r.Image] = true
		}
	}
	for reference := range refs {
		r, digest, err := s.archiveRepository(ctx, reference)
		if err != nil {
			return err
		}
		d, err := b.ExportImage(ctx, r, digest)
		if err != nil {
			return fmt.Errorf("export release image: %w", err)
		}
		m.Images[reference] = d
	}
	return nil
}

var gitArchiveRevision = regexp.MustCompile(`^[a-fA-F0-9]{7,64}$`)

func (s *Server) exportProjectBlob(ctx context.Context, b *projectarchive.Bundle, m *projectArchiveMetadata) error {
	if m.Spec.Source == nil || m.Spec.Source.Blob == nil {
		return nil
	}
	if s.sources == nil {
		return errors.New("source storage is not configured")
	}
	sha := m.Spec.Source.Blob.SHA256
	if !sourceName.MatchString(sha + ".tgz") {
		return errors.New("project source has no valid content digest")
	}
	var r io.ReadCloser
	var err error
	if s.sources.Bucket != nil {
		bucket := s.sources.Bucket
		r, err = bucket.Client.GetObject(ctx, bucket.Bucket, bucket.Key(sha+".tgz"), minio.GetObjectOptions{})
	} else {
		r, err = os.Open(filepath.Join(s.sources.Dir, sha+".tgz"))
	}
	if err != nil {
		return err
	}
	defer r.Close()
	m.SourceEntry = "source/source.tgz"
	return b.Add(ctx, m.SourceEntry, r)
}

func validateProjectArchive(ctx context.Context, b *projectarchive.Bundle) (*projectArchiveMetadata, error) {
	var m projectArchiveMetadata
	if err := b.JSON("project.json", &m); err != nil {
		return nil, err
	}
	if len(m.Volumes) > 100 || len(m.Databases) > 100 {
		return nil, errors.New("too many project resources")
	}
	names := map[string]bool{"project.json": true}
	for _, v := range m.Volumes {
		name := "volumes/" + v.Name + ".tar"
		if !project.ValidSlug(v.Name) || names[name] || v.Spec.Size.Sign() <= 0 {
			return nil, errors.New("invalid volume inventory")
		}
		names[name] = true
		f, err := b.Open(name)
		if err != nil {
			return nil, err
		}
		err = projectarchive.ValidateVolume(ctx, f, projectarchive.DefaultLimit)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("volume %s: %w", v.Name, err)
		}
	}
	for _, pg := range m.Databases {
		name := "databases/" + pg.Name + ".dump"
		if !project.ValidSlug(pg.Name) || names[name] {
			return nil, errors.New("invalid database inventory")
		}
		names[name] = true
		f, err := b.Open(name)
		if err != nil {
			return nil, err
		}
		var magic [5]byte
		_, err = io.ReadFull(f, magic[:])
		f.Close()
		if err != nil || string(magic[:]) != "PGDMP" {
			return nil, fmt.Errorf("invalid PostgreSQL dump for %s", pg.Name)
		}
	}
	if m.SourceEntry != "" {
		if m.SourceEntry != "source/source.tgz" {
			return nil, errors.New("invalid source entry")
		}
		names[m.SourceEntry] = true
		f, err := b.Open(m.SourceEntry)
		if err != nil {
			return nil, err
		}
		f.Close()
	}
	for _, name := range b.Names() {
		if !names[name] && !strings.HasPrefix(name, "image/blobs/sha256/") {
			return nil, fmt.Errorf("unexpected archive entry: %s", name)
		}
	}
	if m.Status.Image != "" {
		if _, ok := m.Images[m.Status.Image]; !ok {
			return nil, errors.New("deployed image missing from archive")
		}
	}
	return &m, nil
}

func addProjectMetadata(ctx context.Context, b *projectarchive.Bundle, m *projectArchiveMetadata) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return b.Add(ctx, "project.json", strings.NewReader(string(data)))
}
