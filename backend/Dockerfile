# ---- build ----
FROM golang:1.27-alpine AS build

WORKDIR /src

# Os módulos e o cache de compilação do Go ficam em caches do BuildKit, fora
# das camadas: cada deploy reaproveita o mesmo cache (compila só o que mudou)
# em vez de gravar uma camada nova de ~600 MB no disco do servidor.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

# Binários estáticos: a imagem final não tem libc.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/server ./cmd/server \
    && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/tksim ./cmd/tksim

# ---- runtime ----
FROM alpine:3.24

RUN apk add --no-cache ca-certificates tzdata wget \
    && adduser -D -u 10001 tracker

COPY --from=build /out/server /usr/local/bin/server
COPY --from=build /out/tksim /usr/local/bin/tksim

USER tracker

# 8080: API HTTP e WebSocket. 5000: rastreadores.
EXPOSE 8080 5000

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["/usr/local/bin/server"]
