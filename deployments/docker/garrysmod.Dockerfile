FROM debian:bookworm-slim
ARG DEBIAN_FRONTEND=noninteractive
RUN dpkg --add-architecture i386 && apt-get update && apt-get install -y --no-install-recommends ca-certificates lib32gcc-s1 libstdc++6:i386 && rm -rf /var/lib/apt/lists/*
WORKDIR /srv/server
CMD ["/srv/server/srcds_run", "-console", "-game", "garrysmod"]
