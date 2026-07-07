# Solo VPN

Solo VPN 是一个自定义 VPN 项目，包含 Android 客户端、Go 服务端和服务端管理面板。

- APK 客户端：使用 Kotlin 和 Android `VpnService`
- 服务端：使用 Go 和 Linux TUN，目标环境为 Ubuntu Server 22.04
- 管理面板：在服务器上构建 APK，并在打包前写入服务器地址

本项目没有使用 WireGuard、OpenVPN 或其他现成 VPN 协议库。当前协议基于 UDP 传输、AES-GCM 加密和预共享密钥，适合作为学习、自托管和后续扩展的原型项目。

## 目录结构

```text
app/                 Android APK 客户端
server/              Ubuntu 服务端、管理面板和部署脚本
server/README.md     服务端部署说明
```

## 服务端构建 APK 流程

1. 将 `server/` 部署到 Ubuntu 22.04。
2. 打开管理面板，填写服务器公网地址。
3. 管理面板会把 Android 内置配置写入 `app/src/main/assets/default_vpn_config.json`。
4. 管理面板执行 `./gradlew :app:assembleDebug --no-daemon` 构建 APK。
5. 构建完成后，管理面板提供 APK 下载链接。

部署步骤请查看 [server/README.md](server/README.md)。
