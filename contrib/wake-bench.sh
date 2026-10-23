#!/usr/bin/env bash
# Measures how long a sleeping project takes to answer its first request
# (RFC-0075, storage plan step 6): N cycles of "put it to sleep, wake it
# with one request, time it", then p50 and p95. Four paths are measured
# with the same script; what differs is the project it is pointed at:
#
#   http            a web process alone
#   http + disk     a web process with a single-instance volume
#   http + db       a web process and its dedicated database, both asleep:
#                   the request's wake and, apart, the first connection's
#                   through the pg-gateway (--postgres <name> --db-client <pod>,
#                   a pod in the namespace with psql, e.g. postgres:17-alpine)
#   http + shared   a web process on a shared database (it never sleeps)
#
# Sleep is forced, not waited for: the web Deployment is scaled to 0 (the
# KEDA interceptor then holds or pages the next request and scales it back),
# and a dedicated database is hibernated by dating its activity back so the
# reconciler puts it to sleep on its next pass, as it would after the quiet
# period. Everything else is the real path: the interceptor, the scaler,
# the pg-gateway, the database's start.
#
#   contrib/wake-bench.sh --context oke-shpyrd-dev --namespace p-<id> \
#     --url https://shop.example/ --cycles 20 [--postgres db --db-client pgclient] [--deployment web]
#
# The request is timed until the first 2xx: in wait mode that is the one
# held request; in page mode the waking page answers at once (503) and the
# script polls every 500 ms until the app answers. Needs kubectl and curl.
set -euo pipefail

context="" namespace="" url="" postgres="" dbclient="" deployment="web" cycles=20
while [ $# -gt 0 ]; do
  case "$1" in
    --context) context=$2; shift 2 ;;
    --namespace) namespace=$2; shift 2 ;;
    --url) url=$2; shift 2 ;;
    --postgres) postgres=$2; shift 2 ;;
    --db-client) dbclient=$2; shift 2 ;;
    --deployment) deployment=$2; shift 2 ;;
    --cycles) cycles=$2; shift 2 ;;
    *) echo "unknown argument $1" >&2; exit 2 ;;
  esac
done
[ -n "$context" ] && [ -n "$namespace" ] && [ -n "$url" ] || { echo "usage: $0 --context <ctx> --namespace <ns> --url <https://host/> [--postgres <name>] [--deployment web] [--cycles 20]" >&2; exit 2; }
k() { kubectl --context "$context" -n "$namespace" "$@"; }
now_ms() { python3 -c 'import time; print(int(time.time()*1000))'; }
note() { printf '\033[1;34m==>\033[0m %s\n' "$*" >&2; }

# sleep_web scales the web process to 0 and waits until no pod of it runs.
# In page mode the scaler acts on the request rate over a minute: a request
# younger than that (the previous wake's polling) brings the pod straight
# back, so the cycles leave more than a minute between the last request and
# the scale-down, and the pod is scaled down once more if it came back.
sleep_web() {
  local attempt
  for attempt in 1 2 3; do
    k scale deployment "$deployment" --replicas=0 >/dev/null
    for _ in $(seq 1 90); do
      [ "$(k get pods -l shpyrd.io/process=web --no-headers 2>/dev/null | wc -l | tr -d ' ')" = 0 ] && return 0
      sleep 1
    done
    note "the web pods came back after the scale-down (attempt $attempt); scaling down again"
  done
  echo "the web pods did not stop" >&2; return 1
}

# sleep_db dates the database's activity back two hours with a fresh check,
# then waits for the reconciler to hibernate it (state sleeping).
sleep_db() {
  local past now
  past=$(date -u -v-2H +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '2 hours ago' +%Y-%m-%dT%H:%M:%SZ)
  now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  k patch postgres "$postgres" --subresource=status --type=merge -p "{\"status\":{\"sleep\":{\"lastActivityAt\":\"$past\",\"activityCheckedAt\":\"$now\"}}}" >/dev/null
  for _ in $(seq 1 240); do
    state=$(k get postgres "$postgres" -o jsonpath='{.status.sleep.state}' 2>/dev/null || true)
    [ "$state" = sleeping ] && break
    sleep 2
  done
  [ "$state" = sleeping ] || { echo "the database did not hibernate: $(k get postgres "$postgres" -o jsonpath='{.status.sleep.message}')" >&2; return 1; }
  # Hibernation is declared when the annotation is set; the pods go a moment later.
  for _ in $(seq 1 120); do
    [ "$(k get pods -l cnpg.io/cluster="$postgres" --no-headers 2>/dev/null | wc -l | tr -d ' ')" = 0 ] && return 0
    sleep 1
  done
  echo "the database's pods did not stop" >&2; return 1
}

# wake_db times the first connection to the sleeping database through its
# sleep-aware Service "<name>" (the pg-gateway holds it, wakes the cluster
# and pipes), from a pod in the namespace with psql. The password travels
# as PGPASSWORD into that pod and is never printed.
wake_db() {
  local start pass
  pass=$(k get secret "$postgres-app" -o jsonpath='{.data.password}' | base64 -d)
  start=$(now_ms)
  if k exec "$dbclient" -- env PGPASSWORD="$pass" PGCONNECT_TIMEOUT=180 psql -h "$postgres" -U app -d app -At -c 'select 1' >/dev/null 2>&1; then
    echo $(( $(now_ms) - start ))
  else
    echo "the database did not answer the first connection" >&2; return 1
  fi
}

# wake sends one request and times until the first 2xx answer.
wake() {
  local start code
  start=$(now_ms)
  for _ in $(seq 1 600); do
    code=$(curl -sk -o /dev/null -w '%{http_code}' --max-time 180 "$url" || echo 000)
    case "$code" in 2*) echo $(( $(now_ms) - start )); return 0 ;; esac
    sleep 0.5
  done
  echo "no 2xx within the window (last code $code)" >&2; return 1
}

[ -z "$postgres" ] || [ -n "$dbclient" ] || { echo "--postgres needs --db-client <pod with psql>" >&2; exit 2; }
times=() dbtimes=()
for i in $(seq 1 "$cycles"); do
  sleep_web
  [ -n "$postgres" ] && sleep_db
  sleep 5 # let the interceptor and the gateway see the state
  t=$(wake)
  times+=("$t")
  if [ -n "$postgres" ]; then
    d=$(wake_db)
    dbtimes+=("$d")
    note "cycle $i: http ${t} ms, database ${d} ms"
  else
    note "cycle $i: ${t} ms"
  fi
  # Let the app settle before the next cycle: past the scaler's one-minute
  # request window, so the next scale-down holds.
  sleep 75
done
# report prints p50, p95, min and max of a list of milliseconds.
report() {
  local label=$1; shift
  local sorted n
  sorted=$(printf '%s\n' "$@" | sort -n)
  n=$#
  p() { printf '%s\n' "$sorted" | awk -v q="$1" -v n="$n" 'NR==int((q*n+0.999)) || (NR==n && int((q*n+0.999))>n) {print; exit}'; }
  echo "$label: p50 $(p 0.5) ms   p95 $(p 0.95) ms   min $(printf '%s\n' "$sorted" | head -1) ms   max $(printf '%s\n' "$sorted" | tail -1) ms   (n=$n)"
}
echo "url: $url  cycles: $cycles"
report "http wake" "${times[@]}"
[ -n "$postgres" ] && report "database wake ($postgres, first connection through the gateway)" "${dbtimes[@]}"
