# syntax=docker/dockerfile:1

# Versao do Go declarada aqui e a mesma do go.mod.
ARG GO_VERSION=1.27.1

# ---------------------------------------------------------------------------
# Stage 1: build
# ---------------------------------------------------------------------------
FROM golang:${GO_VERSION}-alpine AS build

WORKDIR /src

# Dependencias primeiro: a layer de modulos so e invalidada quando o
# go.mod/go.sum mudam, nao a cada alteracao de codigo.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build estatico, sem cgo, para rodar em imagem sem toolchain.
# -trimpath remove caminhos absolutos; -s -w reduz o binario.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/app.bin \
    ./cmd/api

# ---------------------------------------------------------------------------
# Stage 2: runtime
# ---------------------------------------------------------------------------
FROM alpine:3.20 AS runtime

# ca-certificates para HTTPS de saída; tzdata para timestamps consistentes;
# curl para o health check do container.
RUN apk add --no-cache ca-certificates tzdata curl \
    && addgroup -S -g 10001 app \
    && adduser -S -u 10001 -G app app

WORKDIR /app

COPY --from=build /out/app.bin /app/app.bin

USER app:app

EXPOSE 8080

ENTRYPOINT ["/app/app.bin"]