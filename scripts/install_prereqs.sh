#!/usr/bin/env bash
# Installs prerequisites for local development of the migration_queue backend:
# Go 1.26.4 and Docker Engine + Compose plugin (containerized Postgres/dev stack).
#
# Requires sudo. Safe to re-run (skips steps that are already satisfied).

set -euo pipefail

GO_VERSION="1.26.4"
GO_TARBALL="go${GO_VERSION}.linux-amd64.tar.gz"
GO_URL="https://go.dev/dl/${GO_TARBALL}"

echo "==> Checking OS"
if [ ! -f /etc/os-release ] || ! grep -qi ubuntu /etc/os-release; then
  echo "This script targets Ubuntu. Detected:"
  cat /etc/os-release 2>/dev/null || true
  echo "Continuing anyway, but apt-based steps may fail."
fi

echo "==> Installing Go ${GO_VERSION}"
if command -v go >/dev/null 2>&1 && [ "$(go version | awk '{print $3}')" = "go${GO_VERSION}" ]; then
  echo "Go ${GO_VERSION} already installed, skipping."
else
  tmpfile="$(mktemp -t "${GO_TARBALL}.XXXXXX")"
  curl -fsSL "${GO_URL}" -o "${tmpfile}"
  sudo rm -rf /usr/local/go
  sudo tar -C /usr/local -xzf "${tmpfile}"
  rm -f "${tmpfile}"

  if [ ! -f /etc/profile.d/go.sh ]; then
    echo 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin' | sudo tee /etc/profile.d/go.sh >/dev/null
  fi
  export PATH="$PATH:/usr/local/go/bin"
fi
go version

echo "==> Installing Docker Engine + Compose plugin"
if command -v docker >/dev/null 2>&1; then
  echo "Docker already installed, skipping engine install."
else
  sudo apt-get update
  sudo apt-get install -y ca-certificates curl gnupg
  sudo install -m 0755 -d /etc/apt/keyrings
  if [ ! -f /etc/apt/keyrings/docker.gpg ]; then
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
    sudo chmod a+r /etc/apt/keyrings/docker.gpg
  fi
  echo \
    "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
    $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
    sudo tee /etc/apt/sources.list.d/docker.list >/dev/null
  sudo apt-get update
  sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
fi

echo "==> Adding $USER to the docker group (avoids needing sudo for docker commands)"
if id -nG "$USER" | grep -qw docker; then
  echo "$USER is already in the docker group."
else
  sudo usermod -aG docker "$USER"
  echo "Added $USER to the docker group. Log out and back in (or run 'newgrp docker') for this to take effect."
fi

echo "==> Versions"
go version
docker --version
docker compose version

echo "==> Done. If this is the first time you were added to the docker group, start a new shell session before running docker commands without sudo."
