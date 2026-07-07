# Windows EXE 构建

管理面板可以在构建/下载区域同时构建 Android APK 和 Windows 客户端 EXE。

## 服务端构建

1. 确认服务器已安装 Go，并且 `solovpn` 服务可以访问 `go` 命令。
2. 打开管理面板。
3. 设置公网服务器地址。
4. 点击 `构建 Windows EXE`。
5. 构建完成后点击 `下载最新 Windows EXE`。

生成的 EXE 会内置管理 API 地址。用户可以双击打开 GUI，然后在窗口中登录或注册。

也可以使用命令行模式：

```powershell
.\solovpn-client.exe -username USER -password PASS
```

## Windows 运行要求

请以管理员身份运行客户端。将 `wintun.dll` 放在 `solovpn-client.exe` 同目录，或放到系统 DLL 搜索路径中。没有 Wintun 时，Windows 无法创建 VPN 隧道网卡。
