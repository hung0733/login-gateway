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
ask PORT "Port" "8080"
ask PUBLISH "Publish port to host? [y/N]" "N"

while :; do
  read -rsp "Password: " PASSWORD; echo
  read -rsp "Confirm password: " CONFIRM; echo
  [[ -n "$PASSWORD" && "$PASSWORD" == "$CONFIRM" ]] && break
  echo "Passwords are empty or do not match, try again."
done
export PASSWORD TARGET_URL PORT

docker build -t "$IMAGE" .

if docker ps -a --format '{{.Names}}' | grep -qx "$NAME"; then
  read -rp "Container '$NAME' exists. Replace it? [y/N]: " yn
  [[ "$yn" =~ ^[Yy]$ ]] || { echo "Aborted."; exit 1; }
  docker rm -f "$NAME" >/dev/null
fi

args=(-d --name "$NAME" --restart unless-stopped -e PASSWORD -e TARGET_URL -e PORT)
[[ -n "$NETWORK" ]] && args+=(--network "$NETWORK")
[[ "$PUBLISH" =~ ^[Yy]$ ]] && args+=(-p "$PORT:$PORT")

docker run "${args[@]}" "$IMAGE"
echo "Started '$NAME'. Point Nginx Proxy Manager to http://$NAME:$PORT (enable WebSockets Support)."
