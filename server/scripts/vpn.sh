#!/usr/bin/env bash
set -euo pipefail

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

SERVICE_NAME="${SERVICE_NAME:-solovpn}"
CONFIG_PATH="${CONFIG_PATH:-/etc/solovpn/server.json}"
SERVER_BIN="${SERVER_BIN:-/opt/solovpn/solovpn-server}"

log_info() {
  echo -e "${green}[INFO]${plain} $*"
}

log_warn() {
  echo -e "${yellow}[WARN]${plain} $*"
}

log_error() {
  echo -e "${red}[ERR]${plain} $*"
}

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    log_error "请使用 root 运行：sudo vpn"
    exit 1
  fi
}

pause_menu() {
  echo
  read -r -p "按回车返回主菜单: " _
  show_menu
}

check_installed() {
  if [[ ! -x "${SERVER_BIN}" ]]; then
    log_error "未找到 ${SERVER_BIN}，请先安装 Solo VPN"
    return 1
  fi
  return 0
}

show_status() {
  if systemctl is-active --quiet "${SERVICE_NAME}"; then
    echo -e "服务状态: ${green}运行中${plain}"
  else
    echo -e "服务状态: ${yellow}未运行${plain}"
  fi

  if systemctl is-enabled --quiet "${SERVICE_NAME}" 2>/dev/null; then
    echo -e "开机自启: ${green}已启用${plain}"
  else
    echo -e "开机自启: ${yellow}未启用${plain}"
  fi
}

show_config() {
  check_installed || return 1
  "${SERVER_BIN}" -config "${CONFIG_PATH}" -show-config
}

start_service() {
  systemctl start "${SERVICE_NAME}"
  log_info "已启动 ${SERVICE_NAME}"
}

stop_service() {
  systemctl stop "${SERVICE_NAME}"
  log_info "已停止 ${SERVICE_NAME}"
}

restart_service() {
  systemctl restart "${SERVICE_NAME}"
  log_info "已重启 ${SERVICE_NAME}"
}

enable_service() {
  systemctl enable "${SERVICE_NAME}"
  log_info "已设置开机自启"
}

disable_service() {
  systemctl disable "${SERVICE_NAME}"
  log_info "已取消开机自启"
}

show_log() {
  journalctl -u "${SERVICE_NAME}" -e --no-pager -f
}

valid_port() {
  [[ "$1" =~ ^[0-9]+$ ]] && [[ "$1" -ge 1 ]] && [[ "$1" -le 65535 ]]
}

set_vpn_port() {
  check_installed || return 1
  echo
  read -r -p "请输入新的 VPN UDP 端口 [1-65535]: " port
  if [[ -z "${port}" ]]; then
    log_warn "已取消"
    return 0
  fi
  if ! valid_port "${port}"; then
    log_error "端口必须是 1-65535"
    return 1
  fi

  "${SERVER_BIN}" -config "${CONFIG_PATH}" -set-port "${port}"

  if command -v ufw >/dev/null 2>&1; then
    ufw allow "${port}/udp" || true
    log_info "已尝试放行 UDP ${port}"
  else
    log_warn "未检测到 ufw，请手动确认云防火墙和系统防火墙已放行 UDP ${port}"
  fi

  echo
  read -r -p "是否立即重启 ${SERVICE_NAME} 让新端口生效? [Y/n]: " answer
  case "${answer}" in
    n|N)
      log_warn "配置已保存，稍后请运行 sudo systemctl restart ${SERVICE_NAME}"
      ;;
    *)
      restart_service
      ;;
  esac

  log_info "端口已改为 ${port}。重新构建 APK 后，客户端才会使用新端口。"
}

show_usage() {
  cat <<EOF
Solo VPN 管理命令:
  vpn              显示交互菜单
  vpn start        启动服务
  vpn stop         停止服务
  vpn restart      重启服务
  vpn status       查看状态
  vpn log          查看日志
  vpn port         修改 VPN UDP 隧道端口
  vpn config       查看当前配置摘要
EOF
}

show_menu() {
  clear
  echo -e "${green}Solo VPN 管理脚本${plain}"
  echo "------------------------"
  show_status
  echo "------------------------"
  echo -e "${green}0.${plain} 退出"
  echo -e "${green}1.${plain} 启动服务"
  echo -e "${green}2.${plain} 停止服务"
  echo -e "${green}3.${plain} 重启服务"
  echo -e "${green}4.${plain} 查看状态"
  echo -e "${green}5.${plain} 查看日志"
  echo -e "${green}6.${plain} 修改 VPN UDP 隧道端口"
  echo -e "${green}7.${plain} 查看当前配置"
  echo -e "${green}8.${plain} 设置开机自启"
  echo -e "${green}9.${plain} 取消开机自启"
  echo
  read -r -p "请输入选择 [0-9]: " num

  case "${num}" in
    0) exit 0 ;;
    1) start_service; pause_menu ;;
    2) stop_service; pause_menu ;;
    3) restart_service; pause_menu ;;
    4) systemctl status "${SERVICE_NAME}" -l --no-pager; pause_menu ;;
    5) show_log ;;
    6) set_vpn_port; pause_menu ;;
    7) show_config; pause_menu ;;
    8) enable_service; pause_menu ;;
    9) disable_service; pause_menu ;;
    *) log_error "请输入 0-9"; pause_menu ;;
  esac
}

require_root

case "${1:-}" in
  "") show_menu ;;
  start) start_service ;;
  stop) stop_service ;;
  restart) restart_service ;;
  status) systemctl status "${SERVICE_NAME}" -l --no-pager ;;
  log) show_log ;;
  port) set_vpn_port ;;
  config) show_config ;;
  help|-h|--help) show_usage ;;
  *) show_usage; exit 1 ;;
esac
