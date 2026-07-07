# Solo VPN 服务端

目标运行环境：Ubuntu Server 22.04。

服务端包含两部分：

- VPN 数据面：Go 程序创建 Linux TUN 设备，并通过自定义 UDP + AES-GCM 协议转发 Android IP 数据包。
- 管理面板：浏览器面板用于保存构建设置、在服务器上运行 Gradle，并提供 APK 下载链接。构建出的 APK 会预先写入服务器地址。

## 部署

将整个项目目录上传到服务器，然后在项目根目录执行：

```bash
sudo bash server/scripts/install_android_sdk.sh
sudo bash server/scripts/install_service.sh
sudo systemctl enable --now solovpn
```

开放防火墙端口：

```bash
sudo ufw allow 51820/udp
sudo ufw allow 8080/tcp
```

管理面板的用户名和生成的密码保存在：

```bash
sudo cat /etc/solovpn/server.json
```

管理面板地址：

```text
http://SERVER_PUBLIC_IP:8080
```

## 管理命令

安装完成后，可以直接输入：

```bash
sudo vpn
```

进入交互式管理菜单。菜单支持启动、停止、重启、查看状态、查看日志、设置开机自启，以及修改 VPN UDP 隧道端口。

也可以直接使用子命令：

```bash
sudo vpn status
sudo vpn restart
sudo vpn log
sudo vpn port
sudo vpn config
```

修改隧道端口时，`vpn` 命令会同时更新服务端监听端口和写入 APK 的客户端连接端口。端口修改后需要重启 `solovpn`，并重新构建 APK，让客户端使用新端口。

## 在服务器上构建 APK

在管理面板中：

1. 输入服务器公网 IP 或域名。
2. 确认 VPN UDP 端口，默认值为 `51820`。
3. 点击 `Save settings`。
4. 点击 `Build APK on server`。
5. 构建完成后下载最新 APK。

每次构建前，服务端都会重写：

```text
app/src/main/assets/default_vpn_config.json
```

下载到的 APK 会预先填好服务器地址、端口、客户端虚拟 IP、DNS、MTU 和共享密钥。

## 运维操作

查看日志：

```bash
sudo journalctl -u solovpn -f
```

重启服务：

```bash
sudo systemctl restart solovpn
```

这是一个自定义 VPN 原型，不是成熟的商用 VPN。当前还没有包含密钥轮换、多客户端地址分配、重放窗口、漫游策略或完整审计等能力。
