#!/usr/bin/env bash
# Run the acceptance tests against the environment testacc-up.sh started.
#
#   scripts/testacc-up.sh && scripts/testacc.sh [-run TestAccPreset] ; scripts/testacc-down.sh
#
# With Go installed, `go test` runs them. Without it, the test binary is built in
# Docker (build/Dockerfile) for this machine and run here, next to the local
# terraform.
set -euo pipefail
here="$(cd "$(dirname "$0")/.." && pwd)"
state="${STATE_DIR:-$here/.testacc}"
# shellcheck disable=SC1091
source "$state/env"

run="${1:-}"; [ "$run" = "-run" ] && pattern="$2" || pattern="TestAcc"

if command -v go >/dev/null 2>&1; then
  cd "$here" && exec go test ./internal/provider -count=1 -v -timeout 60m -run "$pattern"
fi

case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*) target=windows/amd64; ext=.exe ;;
  Darwin) target="darwin/$(uname -m | sed 's/x86_64/amd64/')"; ext= ;;
  *) target="linux/$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"; ext= ;;
esac
docker buildx build -f "$here/build/Dockerfile" --target dist --build-arg TARGETS="$target" --output "$here/dist" "$here"
cd "$here/internal/provider"
exec "$here/dist/provider_${target%/*}_${target#*/}.test$ext" -test.v -test.count=1 -test.timeout 60m -test.run "$pattern"
