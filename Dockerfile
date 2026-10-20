# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
# Ref to check out from the upstream repo (branch, tag, or commit SHA).
ARG MODULE_REF=main
RUN apk add --no-cache git \
    && git init -q /src \
    && git -C /src remote add origin https://github.com/socketz/smsforwarder-api.git \
    && git -C /src fetch -q --depth 1 origin ${MODULE_REF} \
    && git -C /src checkout -q FETCH_HEAD
WORKDIR /src
# Layer 1: download deps into the cache (go.sum-verified).
RUN go mod download
# Layer 2: build as documented in the README (go build -o smsforwarder-api .).
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/smsforwarder-api .

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