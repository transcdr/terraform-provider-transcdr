# syntax=docker/dockerfile:1
# Development builds without a local Go toolchain (from the repository root):
#   docker buildx build -f build/Dockerfile --target tidy --output . .       # go.mod, go.sum
#   docker buildx build -f build/Dockerfile --target fmt  --output . .       # gofmt -w
#   docker buildx build -f build/Dockerfile --target test --progress=plain . # gofmt check, vet, unit tests
#   docker buildx build -f build/Dockerfile --target dist --output dist .    # provider and test binaries
#   docker buildx build -f build/Dockerfile --target docs --output . .       # docs/ via tfplugindocs
ARG GO_IMAGE=golang:1.24
FROM ${GO_IMAGE} AS base
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download || true
COPY . .

FROM base AS tidied
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go mod tidy

FROM scratch AS tidy
COPY --from=tidied /src/go.mod /src/go.sum /

FROM base AS formatted
RUN gofmt -w main.go internal

FROM scratch AS fmt
COPY --from=formatted /src/main.go /main.go
COPY --from=formatted /src/internal /internal

FROM base AS test
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    test -z "$(gofmt -l main.go internal)" || { gofmt -l main.go internal; exit 1; }; \
    go vet ./... && go test -count=1 ./...

FROM base AS built
ARG TARGETS="linux/amd64 windows/amd64"
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /out && for t in $TARGETS; do \
      os=${t%/*}; arch=${t#*/}; ext=; [ "$os" = windows ] && ext=.exe; \
      GOOS=$os GOARCH=$arch go build -ldflags "-X main.version=$VERSION" -o /out/terraform-provider-transcdr_${os}_${arch}${ext} . && \
      GOOS=$os GOARCH=$arch go test -c -o /out/provider_${os}_${arch}.test${ext} ./internal/provider || exit 1; \
    done

FROM scratch AS dist
COPY --from=built /out/ /

FROM base AS docs-gen
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    apt-get update -qq && apt-get install -y -qq unzip >/dev/null && \
    go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.24.0 generate --provider-name transcdr

FROM scratch AS docs
COPY --from=docs-gen /src/docs /docs
