#!/usr/bin/env bash
# Each measurement gets its own match, session and stock. Only that section is resized.
set -euo pipefail
cd "$(dirname "$0")/.."
export BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
export ITERATIONS_PER_VU="${ITERATIONS_PER_VU:-20}"
result_dir="${RESULT_DIR:-bench/results/$(date -u +%Y%m%dT%H%M%SZ)}"
mkdir -p "$result_dir"
result_dir="$(cd "$result_dir" && pwd)"
jar="$(mktemp)"
trap 'rm -f "$jar"' EXIT

psql_demo() {
  docker compose exec -T db psql -X -qAt -v ON_ERROR_STOP=1 -U tribunpass -d tribunpass "$@"
}

for PROFILE in ${PROFILES:-success contention}; do
  for VUS in ${VUS_LIST:-10 50 100}; do
    export PROFILE VUS
    case "$PROFILE" in
      success) CAPACITY=$((VUS * ITERATIONS_PER_VU));;
      contention) CAPACITY=10;;
      *) echo "Unsupported profile: $PROFILE" >&2; exit 2;;
    esac
    : > "$jar"
    curl -fsS -c "$jar" -X POST "$BASE_URL/v1/demo-sessions" >/dev/null
    SECTION_ID="$(curl -fsS -b "$jar" "$BASE_URL/v1/events" | python3 -c 'import json,sys,uuid; print(uuid.UUID(json.load(sys.stdin)["events"][0]["sections"][0]["id"]))')"
    SESSION_TOKEN="$(awk '$6=="tribunpass_session"{print $7}' "$jar")"
    test -n "$SESSION_TOKEN"
    export SECTION_ID SESSION_TOKEN CAPACITY
    psql_demo -v section="$SECTION_ID" -v capacity="$CAPACITY" <<'SQL'
UPDATE sections SET capacity=:'capacity'::integer, available=:'capacity'::integer WHERE id=:'section'::uuid;
SQL
    prefix="$result_dir/$PROFILE-$VUS"
    curl -fsS "$BASE_URL/metrics" > "$prefix-before.prom"
    export RESULT_PATH="$prefix.json"
    K6_NO_USAGE_REPORT=true k6 run --quiet bench/holds.js
    curl -fsS "$BASE_URL/metrics" > "$prefix-after.prom"
    observed="$(psql_demo -v section="$SECTION_ID" <<'SQL'
SELECT s.capacity=s.available+count(h.id) FILTER (WHERE h.status IN ('HELD','CONFIRMED')) FROM sections s LEFT JOIN holds h ON h.section_id=s.id WHERE s.id=:'section'::uuid GROUP BY s.id;
SQL
)"
    test "$observed" = t || { echo "Stock invariant failed" >&2; exit 1; }
  done
done
