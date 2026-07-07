#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "Please run as root: sudo bash server/scripts/setup_network.sh"
  exit 1
fi

VPN_SUBNET="${1:-10.66.0.0/24}"
OUT_IFACE="${2:-$(ip route show default | awk '/default/ {print $5; exit}')}"

if [[ -z "${OUT_IFACE}" ]]; then
  echo "Cannot detect default network interface. Pass it as the second argument."
  exit 1
fi

sysctl -w net.ipv4.ip_forward=1
cat >/etc/sysctl.d/99-solovpn.conf <<EOF
net.ipv4.ip_forward=1
EOF

if ! iptables -t nat -C POSTROUTING -s "${VPN_SUBNET}" -o "${OUT_IFACE}" -j MASQUERADE 2>/dev/null; then
  iptables -t nat -A POSTROUTING -s "${VPN_SUBNET}" -o "${OUT_IFACE}" -j MASQUERADE
fi

if ! iptables -C FORWARD -s "${VPN_SUBNET}" -j ACCEPT 2>/dev/null; then
  iptables -A FORWARD -s "${VPN_SUBNET}" -j ACCEPT
fi

if ! iptables -C FORWARD -d "${VPN_SUBNET}" -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null; then
  iptables -A FORWARD -d "${VPN_SUBNET}" -m state --state RELATED,ESTABLISHED -j ACCEPT
fi

echo "NAT enabled for ${VPN_SUBNET} via ${OUT_IFACE}"
