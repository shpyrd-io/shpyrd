package install

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// Sources only need scratch space with S3. Existing claims are deliberately
// not deleted by an upgrade: their archives must be copied before cleanup.
func sourcesInBucket(objs []*unstructured.Unstructured, secret string) ([]*unstructured.Unstructured, error) {
	out := make([]*unstructured.Unstructured, 0, len(objs))
	for _, obj := range objs {
		if obj.GetKind() == "PersistentVolumeClaim" && obj.GetName() == "shpyrd-data" {
			continue
		}
		if obj.GetKind() == "Deployment" && obj.GetName() == "shpyrd-server" {
			var dep appsv1.Deployment
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &dep); err != nil {
				return nil, err
			}
			for i := range dep.Spec.Template.Spec.Volumes {
				v := &dep.Spec.Template.Spec.Volumes[i]
				if v.Name == "data" {
					v.VolumeSource = corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}
				}
			}
			for i := range dep.Spec.Template.Spec.Containers {
				c := &dep.Spec.Template.Spec.Containers[i]
				if c.Name == "server" {
					for _, key := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"} {
						c.Env = append(c.Env, corev1.EnvVar{Name: "SHPYRD_SOURCES_" + key, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret}, Key: key}}})
					}
				}
			}
			var err error
			obj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(&dep)
			if err != nil {
				return nil, err
			}
		}
		out = append(out, obj)
	}
	return out, nil
}
