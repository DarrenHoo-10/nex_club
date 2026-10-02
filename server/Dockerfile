FROM node:22-alpine AS web
WORKDIR /build
COPY package.json package-lock.json ./
RUN npm ci
COPY index.html vite.config.js ./
COPY src ./src
COPY public ./public
RUN npm run build

FROM golang:1.25-alpine AS backend
WORKDIR /build/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server ./
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X github.com/darrenhoo/nex_club/server/internal/platform/buildinfo.Version=$VERSION" -o /out/api ./cmd/api \
 && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X github.com/darrenhoo/nex_club/server/internal/platform/buildinfo.Version=$VERSION" -o /out/worker ./cmd/worker \
 && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X github.com/darrenhoo/nex_club/server/internal/platform/buildinfo.Version=$VERSION" -o /out/nexadm ./cmd/nexadm

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
 && addgroup -g 10001 nex \
 && adduser -D -H -u 10001 -G nex nex
WORKDIR /app
COPY --from=backend /out/ ./
COPY --from=web /build/dist ./dist
ENV NEX_HTTP_ADDR=:8080 NEX_STATIC_DIR=/app/dist TZ=UTC
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/app/api"]
