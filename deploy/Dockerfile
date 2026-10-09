# Application image (D-18): one static Go binary, with the web client
# embedded, on a distroless base. Build from the repository root:
#   podman build -f deploy/Dockerfile -t brinketask .
#
# Base images are pinned by digest (multi-arch index), so a rebuild uses the
# same bytes; the tag next to each digest is informational. Update both
# together.

FROM docker.io/library/node:24.21.0-slim@sha256:51b1100cc2a83d370c6a60952e3f2989c8a43159d0e38586e090f3b3326efefd AS web

WORKDIR /src/web

# Install packages in their own layer, so source edits do not invalidate it.
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-fund --no-audit

# The generated API types are committed, so the contract itself is not
# needed here.
COPY web/ ./
RUN npm run build

FROM docker.io/library/golang:1.27.1-trixie@sha256:8f58fd67ea075142d947a60e0caa4317746a55118d312f027793d382c7741734 AS build

# Use the Go of this image; never download another toolchain during the build.
ENV GOTOOLCHAIN=local CGO_ENABLED=0

WORKDIR /src

# Download modules in their own layer, so source edits do not invalidate it.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
COPY migrations/ migrations/
COPY web/*.go web/
COPY --from=web /src/web/dist/ web/dist/
RUN go build -trimpath -tags webui -ldflags="-s -w" -o /out/brinketask ./cmd/brinketask

# distroless/static: no shell, no package manager, CA certificates included.
# The "nonroot" variant runs as UID/GID 65532.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3

COPY --from=build /out/brinketask /brinketask

# The process writes nothing to disk, so the container can run with a
# read-only root file system (SPEC section 10).
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/brinketask"]
