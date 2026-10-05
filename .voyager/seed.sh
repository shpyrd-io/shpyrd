#!/usr/bin/env bash
# Puts the local kind cluster (`make dev-cluster`, auth-local enabled) into a
# named Voyager scenario: wipes everything a journey can see, then creates the
# scenario's fixture through the CLIs and the API, as an operator would.
#
#   .voyager/seed.sh <empty|established-team>
#
# VOYAGER_PASSWORD is every fixture account's password; the journeys sign in
# with it. CLUSTER (default shpyrd) names the kind cluster. It refuses any
# other context: this wipes the workspace.
set -euo pipefail

scenario=${1:-}
case "$scenario" in
  empty | established-team) ;;
  *)
    echo "seed: unknown scenario '${scenario}' (known: empty, established-team)" >&2
    exit 2
    ;;
esac

: "${VOYAGER_PASSWORD:?seed: set VOYAGER_PASSWORD, the fixture accounts password (8 characters or more)}"

root=$(cd "$(dirname "$0")/.." && pwd)
context=kind-${CLUSTER:-shpyrd}
domain=${SHPYRD_DOMAIN:-127.0.0.1.nip.io}
ca=${SHPYRD_DEV_CA:-$HOME/.shpyrd/ca/rootCA.pem}

for bin in shpyrd shpyrd-ctl; do
  [ -x "$root/bin/$bin" ] || { echo "seed: $root/bin/$bin is missing; run 'make cli'" >&2; exit 1; }
done
kubectl config get-contexts "$context" >/dev/null 2>&1 \
  || { echo "seed: no kubeconfig context $context; run 'make dev-cluster'" >&2; exit 1; }

# --context makes both CLIs act as the platform admin through the
# kubeconfig, never through someone's saved login.
sh() { "$root/bin/shpyrd" --context "$context" "$@"; }
ctl() { "$root/bin/shpyrd-ctl" --context "$context" "$@"; }

# The workspace's name has no command of its own: the API, as admin.
rename_workspace() {
  local token
  token=$(kubectl --context "$context" -n shpyrd-system get secret shpyrd-admin-token -o jsonpath='{.data.token}' | base64 -d)
  curl -fsS --cacert "$ca" -X PATCH "https://$domain/api/workspace" \
    -H "X-Shpyrd-Token: $token" -H 'Content-Type: application/json' \
    -d "{\"name\":\"$1\"}" >/dev/null
}

echo "seed: wiping the workspace"

# Projects first: deleting one removes its namespace and its grants, and
# finishes in the background, so wait until every one is gone.
for slug in $(sh projects list --jq '.[].slug'); do
  sh projects destroy "$slug" -y >/dev/null
done
for _ in $(seq 1 120); do
  [ -z "$(sh projects list --jq '.[].slug')" ] \
    && [ -z "$(kubectl --context "$context" get ns -o name | grep '^namespace/p-' || true)" ] \
    && break
  sleep 1
done
[ -z "$(sh projects list --jq '.[].slug')" ] || { echo "seed: projects still deleting after 2 minutes" >&2; exit 1; }

for team in $(sh teams list --jq '.[] | select(.everyone | not) | .name'); do
  sh teams delete "$team" -y >/dev/null
done
for email in $(sh invitations list --jq '.[].email'); do
  sh invitations revoke "$email" >/dev/null
done
# The workspace always keeps an owner: the fixture's owner takes the role
# before everyone else loses theirs.
owner=owner@acme.test
sh people role "$owner" owner >/dev/null
# Forget a sign-in record (last seen), which someone given a role but never
# signed in does not have.
forget() {
  local out
  out=$(sh people forget "$1" 2>&1) || case "$out" in
    *"person not found"*) ;;
    *) echo "$out" >&2; return 1 ;;
  esac
}
for email in $(sh people list --jq '.[].email'); do
  [ "$email" = "$owner" ] || sh people role "$email" none >/dev/null
  forget "$email"
done
for email in $(ctl users list --jq '.[].email'); do
  ctl users rm "$email" >/dev/null
done

account() { # email name role
  ctl users add "$1" --name "$2" --password "$VOYAGER_PASSWORD" >/dev/null
  sh people role "$1" "$3" >/dev/null
}
project() { # slug name description icon color
  sh projects create "$2" --slug "$1" >/dev/null
  sh projects describe "$1" --description "$3" --icon "$4" --color "$5" >/dev/null
}

echo "seed: creating scenario $scenario"
rename_workspace "Acme"
account "$owner" "Olivia Owner" owner

if [ "$scenario" = established-team ]; then
  account ada@acme.test "Ada Admin" admin
  account dev@acme.test "Dev Member" member
  sh teams create platform --description "Keeps the apps running" --member dev@acme.test >/dev/null
  project website "Website" "The public site at acme.test" globe blue
  project api "API" "The JSON API behind the apps" server green
fi

echo "seed: $scenario ready"
