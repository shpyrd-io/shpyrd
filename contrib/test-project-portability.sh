#!/usr/bin/env bash
# An isolated destructive integration test. Never uses the current kubeconfig.
set -euo pipefail
cd "$(dirname "$0")/.."
cluster=shpyrd-portability-test
if kind get clusters | grep -qx "$cluster"; then
  echo "$cluster already exists; refusing to adopt or delete it" >&2
  exit 1
fi
scratch=$(mktemp -d)
created=false
cleanup() {
  if "$created"; then kind delete cluster --name "$cluster"; fi
  rm -rf "$scratch"
}
trap cleanup EXIT
cat > "$scratch/kind.yaml" <<'YAML'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
  - role: worker
  - role: worker
kubeadmConfigPatches:
  - |
    kind: KubeletConfiguration
    failCgroupV1: false
YAML
kind create cluster --name "$cluster" --image kindest/node:v1.37.0 --config "$scratch/kind.yaml" --kubeconfig "$scratch/kubeconfig"
created=true
k=(kubectl --kubeconfig "$scratch/kubeconfig")
"${k[@]}" create namespace shpyrd-system
"${k[@]}" apply -f deploy/components/shpyrd/base/crds/
"${k[@]}" apply -k deploy/components/storage-local/base
"${k[@]}" create namespace cnpg-system
helm template cnpg-test cloudnative-pg --repo https://cloudnative-pg.github.io/charts --version 0.29.0 --namespace cnpg-system --kube-version 1.37.0 --include-crds > "$scratch/cnpg.yaml"
"${k[@]}" apply --server-side -n cnpg-system -f "$scratch/cnpg.yaml"
"${k[@]}" wait --for=condition=Established --timeout=120s crd/clusters.postgresql.cnpg.io
# One operator in this disposable cluster. Avoid lease timeouts on busy laptops.
"${k[@]}" -n cnpg-system patch deployment cnpg-test-cloudnative-pg --type=json -p '[{"op":"replace","path":"/spec/strategy","value":{"type":"Recreate"}},{"op":"replace","path":"/spec/template/spec/containers/0/args/1","value":"--leader-elect=false"},{"op":"replace","path":"/spec/template/spec/containers/0/args/2","value":"--max-concurrent-reconciles=2"}]'
"${k[@]}" -n cnpg-system rollout status deployment/cnpg-test-cloudnative-pg --timeout=300s
case "$(docker info --format '{{.Architecture}}')" in
  arm64|aarch64) target_arch=arm64 ;;
  amd64|x86_64) target_arch=amd64 ;;
  *) echo 'Unsupported Docker architecture' >&2; exit 1 ;;
esac
mkdir "$scratch/image"
GOOS=linux GOARCH="$target_arch" CGO_ENABLED=0 go build -o "$scratch/image/shpyrd-server" ./cmd/shpyrd-server
cat > "$scratch/image/Dockerfile" <<'DOCKER'
FROM alpine:3
COPY shpyrd-server /shpyrd-server
ENTRYPOINT ["/shpyrd-server"]
DOCKER
image=shpyrd-portability-test:latest
docker build -t "$image" "$scratch/image"
kind load docker-image "$image" --name "$cluster"
SHPYRD_ARCHIVE_TEST_KUBECONFIG="$scratch/kubeconfig" SHPYRD_ARCHIVE_TEST_IMAGE="$image" go test ./pkg/api -run '^TestProjectArchiveKubernetesRoundTrip$' -count=1 -v -timeout=40m
