# Solo VPN

Solo VPN is a custom VPN project:

- APK client: Kotlin + Android `VpnService`
- Server: Go + Linux TUN for Ubuntu Server 22.04
- Admin panel: build the APK on the server and embed the server address before packaging

This project does not use WireGuard, OpenVPN, or another VPN protocol library. The current protocol uses UDP transport, AES-GCM encryption, and a pre-shared key. It is a learning and self-hosted prototype that can be extended.

## Layout

```text
app/                 Android APK client
server/              Ubuntu server, admin panel, and deployment scripts
server/README.md     Server deployment guide
```

## Server-side APK build flow

1. Deploy `server/` on Ubuntu 22.04.
2. Open the admin panel and enter the public server host.
3. The panel writes the Android built-in config to `app/src/main/assets/default_vpn_config.json`.
4. The panel runs `./gradlew :app:assembleDebug --no-daemon`.
5. The panel provides a download link for the generated APK.

See [server/README.md](server/README.md) for deployment steps.
