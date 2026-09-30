# shpyrd-server: API + App controller + the embedded applications.
#
#   docker build -t shpyrd-server:dev .
#   docker buildx build --platform linux/amd64,linux/arm64 -t ghcr.io/shpyrd-io/shpyrd-server:vX.Y.Z .
#
# Stage 1 builds the applications (the npm workspace at the root: the
# library in design/, the applications in apps/), stage 2 embeds them into
# the Go binary, stage 3 is a distroless runtime image. Stages 1 and 2 run
# on the build platform and cross-compile for the target, so a multi-arch
# build needs no emulation. .dockerignore keeps node_modules, the built
# files and the website out of the context.

FROM --platform=$BUILDPLATFORM node:24-alpine AS ui
WORKDIR /src
COPY package.json package-lock.json ./
COPY design/ui/package.json design/ui/
COPY design/pages/package.json design/pages/
COPY apps/shared/package.json apps/shared/
COPY apps/example/package.json apps/example/
COPY apps/console/package.json apps/console/
COPY apps/workspace/package.json apps/workspace/
RUN npm ci --no-audit --no-fund
COPY design/ design/
COPY apps/ apps/
RUN npm --prefix apps/console run build && npm --prefix apps/workspace run build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/apps/console/out ./pkg/ui/dist/console
COPY --from=ui /src/apps/workspace/out ./pkg/ui/dist/workspace
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -trimpath \
      -ldflags "-s -w -X github.com/shpyrd-io/shpyrd/pkg/version.Version=${VERSION}" \
      -o /out/shpyrd-server ./cmd/shpyrd-server && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -trimpath \
      -ldflags "-s -w -X github.com/shpyrd-io/shpyrd/pkg/version.Version=${VERSION}" \
      -o /out/pg-gateway ./cmd/pg-gateway

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/shpyrd-server /shpyrd-server
# The PostgreSQL wake-on-connect proxy (RFC-0075) rides in the same image;
# the pg-gateway component runs it with args [/pg-gateway].
COPY --from=build /out/pg-gateway /pg-gateway
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/shpyrd-server"]
