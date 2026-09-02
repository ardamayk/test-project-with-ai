#!/usr/bin/env bash
set -euo pipefail

sudo apt-get update
sudo apt-get install -y \
  build-essential \
  ffmpeg \
  libayatana-appindicator3-dev \
  libass-dev \
  libavcodec-dev \
  libavfilter-dev \
  libavformat-dev \
  libavutil-dev \
  libplacebo-dev \
  librsvg2-dev \
  libssl-dev \
  libswresample-dev \
  libswscale-dev \
  libwebkit2gtk-4.1-dev \
  libxdo-dev \
  meson \
  ninja-build \
  pkg-config
