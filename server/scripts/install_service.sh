#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "Please run as root: sudo bash server/scripts/install_service.sh"
  exit 1
fi

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
SERVER_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
PROJECT_ROOT="$(cd -- "${SERVER_DIR}/.." && pwd)"
INSTALL_ROOT="${INSTALL_ROOT:-/opt/solovpn}"
PROJECT_TARGET="${INSTALL_ROOT}/project"
CONFIG_PATH="${CONFIG_PATH:-/etc/solovpn/server.json}"

apt-get update
apt-get install -y ca-certificates golang-go openjdk-17-jdk iproute2 iptables tar

mkdir -p "${INSTALL_ROOT}" "${INSTALL_ROOT}/builds" /etc/solovpn
rm -rf "${PROJECT_TARGET}"
mkdir -p "${PROJECT_TARGET}"

tar \
  --exclude='.gradle' \
  --exclude='build' \
  --exclude='app/build' \
  --exclude='server/builds' \
  -C "${PROJECT_ROOT}" \
  -cf - . | tar -C "${PROJECT_TARGET}" -xf -

chmod +x "${PROJECT_TARGET}/gradlew" "${PROJECT_TARGET}/server/scripts/"*.sh

(
  cd "${PROJECT_TARGET}/server"
  go build -o "${INSTALL_ROOT}/solovpn-server" ./cmd/solovpn-server
)

"${INSTALL_ROOT}/solovpn-server" -config "${CONFIG_PATH}" -init-config
install -m 0755 "${PROJECT_TARGET}/server/scripts/vpn.sh" /usr/bin/vpn
cp "${PROJECT_TARGET}/server/systemd/solovpn.service" /etc/systemd/system/solovpn.service

systemctl daemon-reload

echo
echo "Solo VPN installed."
echo "Config: ${CONFIG_PATH}"
echo "Admin panel: http://SERVER_IP:8080"
echo "Management command: sudo vpn"
echo "Start service: sudo systemctl enable --now solovpn"
echo "Open firewall ports: UDP 51820 and TCP 8080"
