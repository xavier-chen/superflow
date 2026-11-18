ARG BASE=golang:1.26.4-bookworm
FROM ${BASE}
USER root
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc libc6-dev pkg-config libgl1-mesa-dev libx11-dev libxcursor-dev libxrandr-dev \
    libxinerama-dev libxi-dev libxxf86vm-dev libxkbcommon-dev libwayland-dev \
    xvfb xauth dbus-x11 openbox fonts-noto-cjk libopus0 gcc-mingw-w64-x86-64 desktop-file-utils \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /work
ENV LIBGL_ALWAYS_SOFTWARE=1
ENV PATH=/usr/local/go/bin:$PATH
