#!/usr/bin/env sh
set -eu

REPO_URL="${1:-${SPARKD_REPO_URL:-https://github.com/shellhaki/sparkd.git}}"
INSTALL_DIR="${SPARKD_INSTALL_DIR:-/opt/sparkd}"
BIN_PATH="${SPARKD_BIN_PATH:-/usr/local/bin/sparkd}"
SERVICE_PATH="${SPARKD_SERVICE_PATH:-/etc/systemd/system/sparkd.service}"
ENV_PATH="${SPARKD_ENV_PATH:-/etc/sparkd.env}"
SPARKD_ADDR="${SPARKD_ADDR:-0.0.0.0:8721}"
SPARKD_HOST="${SPARKD_HOST:-}"

if [ "$(id -u)" -ne 0 ]; then
  echo "Please run as root: sudo sh scripts/install-systemd.sh"
  exit 1
fi

need_cmd() {
  command -v "$1" >/dev/null 2>&1
}

install_packages() {
  if need_cmd git && need_cmd go; then
    return
  fi

  echo "[*] Installing required packages: git, go"
  if need_cmd apt-get; then
    apt-get update
    apt-get install -y git golang-go
  elif need_cmd dnf; then
    dnf install -y git golang
  elif need_cmd yum; then
    yum install -y git golang
  elif need_cmd apk; then
    apk add --no-cache git go
  else
    echo "Could not find a supported package manager. Install git and Go, then rerun this script."
    exit 1
  fi
}

install_packages

echo "[*] Using repo: $REPO_URL"
echo "[*] Installing source into: $INSTALL_DIR"

if [ -d "$INSTALL_DIR/.git" ]; then
  git -C "$INSTALL_DIR" fetch --all --prune
  git -C "$INSTALL_DIR" pull --ff-only
else
  rm -rf "$INSTALL_DIR"
  git clone "$REPO_URL" "$INSTALL_DIR"
fi

echo "[*] Building SparkD"
(
  cd "$INSTALL_DIR"
  go build -o sparkd .
)

install -m 0755 "$INSTALL_DIR/sparkd" "$BIN_PATH"

echo "[*] Writing environment file: $ENV_PATH"
mkdir -p "$(dirname "$ENV_PATH")"
cat >"$ENV_PATH" <<EOF
SPARKD_ADDR=$SPARKD_ADDR
SPARKD_HOST=$SPARKD_HOST
EOF

echo "[*] Writing systemd unit: $SERVICE_PATH"
cat >"$SERVICE_PATH" <<EOF
[Unit]
Description=SparkD database cell daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=$ENV_PATH
ExecStart=$BIN_PATH daemon
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now sparkd

echo
echo "SparkD installed and started."
echo
echo "Default API:"
echo "  http://<server-ip>:${SPARKD_ADDR##*:}"
echo
echo "Useful commands:"
echo "  sudo systemctl start sparkd"
echo "  sudo systemctl stop sparkd"
echo "  sudo systemctl restart sparkd"
echo "  sudo systemctl status sparkd"
echo "  sudo journalctl -u sparkd -f"
echo "  sudo journalctl -u sparkd --since '1 hour ago'"
echo
echo "Quick API checks:"
echo "  curl http://127.0.0.1:${SPARKD_ADDR##*:}/health"
echo "  curl http://127.0.0.1:${SPARKD_ADDR##*:}/list"
echo
echo "Config file:"
echo "  sudo nano $ENV_PATH"
echo "  sudo systemctl restart sparkd"
