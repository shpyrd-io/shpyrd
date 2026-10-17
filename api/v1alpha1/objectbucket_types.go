package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ObjectBucket phases.
const (
	BucketPending = "Pending"
	BucketReady   = "Ready"
	BucketFailed  = "Failed"
)

// LabelBucketPostgres marks the bucket a database's backups go to (the
// Postgres controller sets it, value: the database's name): the
// platform's bucket, not one of the project's.
const LabelBucketPostgres = "shpyrd.io/postgres"

// ObjectBucketSpec asks the platform's object store (RFC-0046) for a bucket
// with a credential that opens it and nothing else. The bucket is named
// after the resource and its namespace; the credential lands in a Secret
// next to the resource.
type ObjectBucketSpec struct {
	// SecretName of the credential Secret written in the resource's
	// namespace (default: <name>-object-storage).
	// +optional
	SecretName string `json:"secretName,omitempty"`
	// RetentionDays expires objects older than this (0: keep everything).
	// +optional
	// +kubebuilder:validation:Minimum=0
	RetentionDays int32 `json:"retentionDays,omitempty"`
	// Versioning keeps previous versions of overwritten objects. The
	// gateway refuses it in words (the provider's own versioning is the
	// platform's undo); Garage ignores it.
	// +optional
	Versioning bool `json:"versioning,omitempty"`
	// MaxBytes is what the bucket may hold (storage plan step 5): the
	// gateway refuses an upload that would pass it. Unset: no cap.
	// +optional
	MaxBytes *resource.Quantity `json:"maxBytes,omitempty"`
	// Public lets anyone read an object by its address, without a
	// credential; listing and writing still need the key.
	// +optional
	Public bool `json:"public,omitempty"`
	// Alias is the logical bucket in the store whose objects this bucket
	// serves: a bucket restored into another project keeps the files under
	// the name the first project gave them. Empty: its own.
	// +optional
	Alias string `json:"alias,omitempty"`
	// DeletionPolicy says what happens to the bucket's contents when the
	// resource is deleted: Delete (default) removes the bucket and its
	// objects, Retain leaves them for an operator to remove.
	// +optional
	// +kubebuilder:validation:Enum=Delete;Retain
	DeletionPolicy string `json:"deletionPolicy,omitempty"`
}

// ObjectBucketStatus reports the bucket and its usage.
type ObjectBucketStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase is Pending, Ready or Failed.
	// +optional
	Phase string `json:"phase,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
	// Bucket is the bucket's name in the store.
	// +optional
	Bucket string `json:"bucket,omitempty"`
	// Endpoint is the S3 endpoint URL consumers use.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`
	// PublicEndpoint is the S3 endpoint reachable from outside the
	// cluster (presigned addresses, public objects), when the cluster
	// has one.
	// +optional
	PublicEndpoint string `json:"publicEndpoint,omitempty"`
	// Region is the signing region consumers put in their configuration.
	// +optional
	Region string `json:"region,omitempty"`
	// SecretName holds the credential (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY,
	// AWS_ENDPOINT_URL, AWS_PUBLIC_ENDPOINT_URL, BUCKET, AWS_REGION;
	// accessKeyId and secretAccessKey).
	// +optional
	SecretName string `json:"secretName,omitempty"`
	// Rotated is the stamp of the last rotation applied (the API's
	// shpyrd.io/rotate annotation): equal to the annotation once the new
	// credential is written.
	// +optional
	Rotated string `json:"rotated,omitempty"`
	// UsedBytes and Objects are the last measured usage.
	// +optional
	UsedBytes int64 `json:"usedBytes,omitempty"`
	// +optional
	Objects int64 `json:"objects,omitempty"`
	// +optional
	MeasuredAt *metav1.Time `json:"measuredAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=bucket
// +kubebuilder:printcolumn:name="Bucket",type=string,JSONPath=`.status.bucket`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Used",type=integer,JSONPath=`.status.usedBytes`
// +kubebuilder:printcolumn:name="Cap",type=string,JSONPath=`.spec.maxBytes`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ObjectBucket is a bucket in the platform's object store with its own
// credential (RFC-0046).
type ObjectBucket struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ObjectBucketSpec   `json:"spec,omitempty"`
	Status ObjectBucketStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ObjectBucketList contains a list of ObjectBucket.
type ObjectBucketList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ObjectBucket `json:"items"`
}

// BucketName is the bucket's name in the store: shpyrd-<namespace>-<name>,
// which is unique across the cluster and valid for S3 (lowercase, dashes).
func (b *ObjectBucket) BucketName() string { return "shpyrd-" + b.Namespace + "-" + b.Name }

// PlatformBucket says the platform made this bucket for itself (a
// database's backups): it is not listed, counted, attached or archived
// as the project's.
func (b *ObjectBucket) PlatformBucket() bool { return b.Labels[LabelBucketPostgres] != "" }

// CapBytes is the bucket's cap in bytes, 0 when it has none.
func (b *ObjectBucket) CapBytes() int64 {
	if b.Spec.MaxBytes == nil {
		return 0
	}
	return b.Spec.MaxBytes.Value()
}

// CredentialSecretName is the Secret the credential is written to.
func (b *ObjectBucket) CredentialSecretName() string {
	if b.Spec.SecretName != "" {
		return b.Spec.SecretName
	}
	return b.Name + "-object-storage"
}

func init() {
	SchemeBuilder.Register(&ObjectBucket{}, &ObjectBucketList{})
}
