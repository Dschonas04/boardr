FROM golang:1.27.4-alpine AS bau
WORKDIR /quelle
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=entwicklung
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /boardr .

FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 boardr \
 && mkdir /einstellungen && chown boardr /einstellungen
ENV TZ=Europe/Berlin BOARDR_SSO_DATEI=/einstellungen/sso.json
USER boardr
COPY --from=bau /boardr /usr/local/bin/boardr
EXPOSE 8390
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://127.0.0.1:8390/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/boardr"]
