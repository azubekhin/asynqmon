FROM --platform=$BUILDPLATFORM node:22-bookworm-slim AS frontend
WORKDIR /static
# Required by the existing webpack/minifier dependencies on OpenSSL 3.
ENV NODE_OPTIONS=--openssl-legacy-provider
RUN npm install --global yarn@1.22.22 --force
COPY ui .
# Dependency caches are temporary; only the compiled UI is needed in later stages.
RUN --mount=type=tmpfs,target=/static/node_modules --mount=type=tmpfs,target=/usr/local/share/.cache/yarn \
    yarn install --frozen-lockfile --non-interactive && yarn build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /static/build ui/build
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=tmpfs,target=/tmp --mount=type=tmpfs,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o asynqmon ./cmd/asynqmon

FROM scratch
COPY --from=backend /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=backend /build/asynqmon /
ENTRYPOINT ["/asynqmon"]
