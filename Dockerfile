# syntax=docker/dockerfile:1
# Build context: repository root (Dokploy default: Dockerfile path "Dockerfile", context ".").
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go test ./... \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /analytics . \
 && mkdir -p /out/data && chown 65534:65534 /out/data

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /analytics /analytics
COPY --from=build --chown=65534:65534 /out/data /data
USER 65534:65534
ENV DB_PATH=/data/analytics.db ADDR=:8080
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s CMD ["/analytics", "healthcheck"]
ENTRYPOINT ["/analytics"]
