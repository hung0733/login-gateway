#!/usr/bin/env bash
# Interactively configure, build and start the login gateway container.
set -euo pipefail
cd "$(dirname "$0")"

ask() { # ask <var> <prompt> <default>
  local value
  read -rp "$2 [$3]: " value
  printf -v "$1" '%s' "${value:-$3}"
}

ask NAME "Container name" "login-gateway"
ask IMAGE "Image tag" "login-gateway:latest"
ask TARGET_URL "Upstream URL" "http://firefox:3000"
ask NETWORK "Docker network shared with upstream and proxy (empty = default bridge)" ""
ask HOST_PORT "Host port to publish (empty = do not publish)" ""

while :; do
  read -rsp "Password: " PASSWORD; echo
  read -rsp "Confirm password: " CONFIRM; echo
  [[ -n "$PASSWORD" && "$PASSWORD" == "$CONFIRM" ]] && break
  echo "Passwords are empty or do not match, try again."
done
export PASSWORD TARGET_URL

docker build -t "$IMAGE" .

if docker ps -a --format '{{.Names}}' | grep -qx "$NAME"; then
  read -rp "Container '$NAME' exists. Replace it? [y/N]: " yn
  [[ "$yn" =~ ^[Yy]$ ]] || { echo "Aborted."; exit 1; }
  docker rm -f "$NAME" >/dev/null
fi

args=(-d --name "$NAME" --restart unless-stopped -e PASSWORD -e TARGET_URL)
[[ -n "$NETWORK" ]] && args+=(--network "$NETWORK")
[[ -n "$HOST_PORT" ]] && args+=(-p "$HOST_PORT:8080")

docker run "${args[@]}" "$IMAGE"
echo "Started '$NAME'. Point Nginx Proxy Manager to http://$NAME:8080 (enable WebSockets Support)."
