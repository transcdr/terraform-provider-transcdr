#!/usr/bin/env bash
# Start what the acceptance tests need, isolated from any other local setup:
#   - LocalStack (S3, SQS, SNS, STS) on :4566, with path-style queue URLs
#     (http://localhost:4566/000000000000/<queue>)
#   - the Transcdr API from a checkout of the transcdr repository, on its own
#     SQLite database and port (default 18080), with the demo organization
# and write the environment to use to $STATE_DIR/env:
#   TRANSCDR_BASE_URL, TRANSCDR_API_KEY, TRANSCDR_LOCALSTACK_URL, TF_ACC=1
#
#   TRANSCDR_REPO=../transcdr scripts/testacc-up.sh && source .testacc/env
#   scripts/testacc-down.sh
set -euo pipefail

here="$(cd "$(dirname "$0")/.." && pwd)"
repo="${TRANSCDR_REPO:-$here/../transcdr}"
port="${TRANSCDR_TEST_PORT:-18080}"
state="${STATE_DIR:-$here/.testacc}"
localstack_image="${LOCALSTACK_IMAGE:-localstack/localstack:4}"
bin="$repo/target/debug/transcdr"
[ -x "$bin.exe" ] && bin="$bin.exe"

mkdir -p "$state/storage"
rm -f "$state/transcdr.sqlite"*

if [ ! -x "$bin" ] || [ -n "${BUILD:-}" ]; then
  echo "Building the API in $repo (cargo build -p transcdr-api)…"
  (cd "$repo" && cargo build -p transcdr-api)
fi

echo "Starting LocalStack…"
docker rm -f tf-transcdr-localstack >/dev/null 2>&1 || true
docker run -d --name tf-transcdr-localstack -p 4566:4566 \
  -e SERVICES=s3,sqs,sns,sts,iam -e SQS_ENDPOINT_STRATEGY=off \
  "$localstack_image" >/dev/null

export DATABASE_URL="sqlite://$state/transcdr.sqlite?mode=rwc"
export STORAGE_ROOT="$state/storage" SERVER_PORT="$port" SERVER_HOST=0.0.0.0
export APP_URL="http://localhost:$port" WORKER_API_URL="http://localhost:$port"
export DISPATCH_DRIVER=none LOG_FORMAT=json RUST_LOG=warn
cd "$repo"
"$bin" migrate >/dev/null 2>&1
key="$("$bin" demo:seed 2>/dev/null | sed -n 's/^Live key: *//p')"
[ -n "$key" ] || { echo "demo:seed printed no key" >&2; exit 1; }
# Credit, so the jobs the automations create are accepted.
"$bin" billing:account demo-studio --grant=10000 >/dev/null 2>&1

echo "Starting the API on :$port…"
nohup "$bin" serve --port="$port" >"$state/api.log" 2>&1 &
echo $! >"$state/api.pid"

for _ in $(seq 60); do
  curl -fs "http://localhost:$port/v1/status" >/dev/null 2>&1 && break
  sleep 1
done
curl -fs "http://localhost:$port/v1/status" >/dev/null || { echo "The API did not start; see $state/api.log" >&2; exit 1; }
for _ in $(seq 60); do
  curl -fs http://localhost:4566/_localstack/health | grep -q '"sqs": "\(available\|running\)"' && break
  sleep 1
done

cat >"$state/env" <<ENV
export TF_ACC=1
export TRANSCDR_BASE_URL=http://localhost:$port
export TRANSCDR_API_KEY=$key
export TRANSCDR_LOCALSTACK_URL=http://localhost:4566
ENV
echo "Ready. source $state/env"
