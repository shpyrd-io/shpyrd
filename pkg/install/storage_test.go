package install

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func TestBucketStorageDoesNotAllocateBlockVolumes(t *testing.T) {
	for _, profile := range []string{"local", "oci", "aws"} {
		for _, useS3 := range []bool{false, true} {
			t.Run(profile+"/s3="+map[bool]string{true: "yes", false: "no"}[useS3], func(t *testing.T) {
				vars := map[string]string{}
				if useS3 {
					vars[VarRegistryBucket] = "images"
					vars[VarRegistryEndpoint] = "https://objects.example.com"
					vars[VarRegistryRegion] = "region-1"
				}
				e, err := New(nil, Options{Profile: profile, Vars: vars, Reporter: &quiet{}})
				if err != nil {
					t.Fatal(err)
				}
				for _, component := range []string{"registry", ServerComponent} {
					objects, err := e.renderComponent(e.components[component])
					if err != nil {
						t.Fatal(err)
					}
					wantPVC := !useS3 || (component == ServerComponent && profile == "local")
					claim := "registry-data"
					if component == ServerComponent {
						claim = "shpyrd-data"
					}
					var foundPVC, foundMount bool
					for _, obj := range objects {
						if obj.GetKind() == "PersistentVolumeClaim" && obj.GetName() == claim {
							foundPVC = true
						}
						if obj.GetKind() != "Deployment" {
							continue
						}
						raw := storageYAML(t, obj)
						foundMount = strings.Contains(raw, "claimName: "+claim)
						if component == "registry" && useS3 && strings.Contains(raw, "mountPath: /var/lib/registry") {
							t.Error("S3 registry still mounts data")
						}
						if component == ServerComponent && !wantPVC {
							for _, want := range []string{"emptyDir:", "SHPYRD_SOURCES_AWS_ACCESS_KEY_ID", "SHPYRD_SOURCES_AWS_SECRET_ACCESS_KEY", "name: registry-s3"} {
								if !strings.Contains(raw, want) {
									t.Errorf("server missing %s", want)
								}
							}
						}
					}
					if foundPVC != wantPVC || foundMount != wantPVC {
						t.Errorf("%s: PVC=%v mount=%v want %v", component, foundPVC, foundMount, wantPVC)
					}
					if component == "registry" {
						for _, obj := range objects {
							if obj.GetKind() != "ConfigMap" || obj.GetName() != "registry-config" {
								continue
							}
							config, _, _ := unstructured.NestedString(obj.Object, "data", "config.yml")
							if strings.Contains(config, "s3:") != useS3 || strings.Contains(config, "filesystem:") == useS3 {
								t.Errorf("wrong registry driver: %s", config)
							}
						}
					}
				}
			})
		}
	}
}

func TestSourcesMayUseTheirOwnBucketOrKeepTheFilesystem(t *testing.T) {
	for _, bucket := range []string{"", "separate-sources"} {
		e, err := New(nil, Options{Profile: "oci", Vars: map[string]string{
			VarRegistryBucket: "images", VarRegistryRegion: "region-1", VarRegistryEndpoint: "https://objects.example.com",
			VarSourcesBucket: bucket, VarSourcesSecret: "sources-key",
		}, Reporter: &quiet{}})
		if err != nil {
			t.Fatal(err)
		}
		objs, err := e.renderComponent(e.components[ServerComponent])
		if err != nil {
			t.Fatal(err)
		}
		if e.vars[VarSourcesBucket] != bucket {
			t.Fatal("explicit sources bucket overridden")
		}
		for _, obj := range objs {
			if obj.GetKind() == "Deployment" && bucket != "" && !strings.Contains(storageYAML(t, obj), "name: sources-key") {
				t.Error("dedicated source credential not used")
			}
			if obj.GetKind() == "PersistentVolumeClaim" && obj.GetName() == "shpyrd-data" && bucket != "" {
				t.Error("unexpected sources claim")
			}
		}
	}
}

func storageYAML(t *testing.T, obj *unstructured.Unstructured) string {
	t.Helper()
	b, err := yaml.Marshal(obj.Object)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGatewayRoutesEveryConsumerAndKeepsProviderKeyPrivate(t *testing.T) {
	e, err := New(nil, Options{Profile: "oci", Vars: map[string]string{VarGatewayBucket: "shared-objects", VarGatewayEndpoint: "https://cloud.example", VarGatewayRegion: "region-1"}, Extensions: []ExtensionComponent{{Extension: "object-storage", Component: "object-storage", Runlevel: "rc3"}}, Reporter: &quiet{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{VarRegistryBucket, "registry"}, {VarSourcesBucket, "sources"}, {VarSourcesSecret, "gateway-sources"}, {VarBackupTarget, "s3://platform-backups/platform"}} {
		if e.vars[pair[0]] != pair[1] {
			t.Errorf("%s=%q", pair[0], e.vars[pair[0]])
		}
	}
	for _, key := range []string{VarRegistryEndpoint, VarSourcesEndpoint, VarBackupEndpoint} {
		if e.vars[key] != "http://object-storage.shpyrd-system.svc:3900" {
			t.Errorf("consumer bypasses gateway: %s", key)
		}
	}
	for _, rl := range e.profile.Runlevels {
		for _, c := range e.selected(rl) {
			if c.Name == "object-storage" {
				t.Fatal("cloud gateway also installs Garage")
			}
			if c.Name == "object-gateway" && rl.Name != "rc1" {
				t.Fatal("gateway not available before registry")
			}
		}
	}
	for _, name := range []string{"object-gateway", "registry", ServerComponent, "platform-backup"} {
		objs, err := e.renderComponent(e.components[name])
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range objs {
			raw := storageYAML(t, o)
			if o.GetKind() == "PersistentVolumeClaim" {
				t.Errorf("%s allocates PVC", name)
			}
			if strings.Contains(raw, GatewayBackendSecret) && name != "object-gateway" {
				t.Errorf("provider credential exposed to %s", name)
			}
		}
	}
}
