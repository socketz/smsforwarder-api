# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS build
# Ref to build from the upstream repo directly (no git clone / no local COPY).
ARG MODULE_REF=main
ENV GOBIN=/out \
	CGO_ENABLED=0 \
	GOOS=linux
WORKDIR /src
# Layer 1: download module + deps into the cache (go.sum-verified).
RUN go mod download github.com/shuttlelab/smsforwarder-api@${MODULE_REF}
# Layer 2: build the binary into $GOBIN (/out/smsforwarder-api).
RUN go install -p 2 -trimpath -ldflags="-s -w" github.com/shuttlelab/smsforwarder-api@${MODULE_REF}

FROM alpine:3.20
RUN apk add --no-cache wget ca-certificates && adduser -D -u 10001 sfwd
COPY --from=build /out/smsforwarder-api /usr/local/bin/smsforwarder-api
RUN mkdir -p /data && chown sfwd:sfwd /data
USER sfwd
WORKDIR /app
ENV DB_PATH=/data/smsforwarder.db HTTP_ADDR=0.0.0.0:8090 ADMIN_ADDR=127.0.0.1:8091
EXPOSE 8090
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8090/healthz >/dev/null 2>&1 || exit 1
ENTRYPOINT ["/usr/local/bin/smsforwarder-api"]