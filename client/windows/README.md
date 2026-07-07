# Solo VPN Windows Client

This folder builds a Windows exe client for the existing Solo VPN UDP + AES-GCM protocol.

## Build

From the repo root:

```powershell
client\windows\build.ps1
```

The output is:

```text
client\windows\solovpn-client.exe
```

## Runtime requirements

- Run the exe as Administrator.
- Put `wintun.dll` next to `solovpn-client.exe`, or somewhere in the system DLL search path.
- The server must already be running and reachable on the configured UDP port.

`wintun.dll` is the WireGuard/Wintun virtual network adapter runtime. Without it, Windows cannot create the TUN interface needed for a real VPN tunnel.

## Usage

Double-click `solovpn-client.exe` to open the Windows GUI. The GUI supports the same core flow as the Android APK:

- server API address
- login
- register
- remaining minutes display
- connect VPN
- disconnect VPN

The command-line mode is still available when arguments are passed.

Fetch config from the existing admin API account flow:

```powershell
.\solovpn-client.exe -api http://YOUR_SERVER_IP:8080 -username USER -password PASS
```

Or run with a local config:

```powershell
Copy-Item .\sample-config.json .\solovpn-client.json
notepad .\solovpn-client.json
.\solovpn-client.exe -config .\solovpn-client.json
```

Press `Ctrl+C` to stop. The client removes the routes it added on exit.
