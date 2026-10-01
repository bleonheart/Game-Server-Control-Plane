FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM debian:bookworm-slim
ARG DEBIAN_FRONTEND=noninteractive
RUN dpkg --add-architecture i386 && apt-get update && apt-get install -y --no-install-recommends ca-certificates curl docker.io lib32gcc-s1 libstdc++6:i386 && rm -rf /var/lib/apt/lists/*
RUN mkdir -p /opt/steamcmd && curl -fsSL https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz | tar -xz -C /opt/steamcmd && ln -s /opt/steamcmd/steamcmd.sh /usr/local/bin/steamcmd
COPY --from=build /out/api /usr/local/bin/api
COPY --from=build /out/worker /usr/local/bin/worker
COPY --from=build /out/migrate /usr/local/bin/migrate
RUN mkdir -p /var/lib/game-control-plane/servers /var/lib/game-control-plane/backups
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/api"]
