# Solo VPN Server

Target runtime: Ubuntu Server 22.04.

The server has two parts:

- VPN data plane: a Go program creates a Linux TUN device and forwards Android IP packets through a custom UDP + AES-GCM protocol.
- Admin panel: a browser panel saves build settings, runs Gradle on the server, and provides an APK download link. The APK is built with the server address already embedded.

## Deploy

Upload the whole project directory to the server, then run these commands from the project root:

```bash
sudo bash server/scripts/install_android_sdk.sh
sudo bash server/scripts/install_service.sh
sudo systemctl enable --now solovpn
```

Open firewall ports:

```bash
sudo ufw allow 51820/udp
sudo ufw allow 8080/tcp
```

The admin username and generated password are stored in:

```bash
sudo cat /etc/solovpn/server.json
```

Admin panel:

```text
http://SERVER_PUBLIC_IP:8080
```

## Build APK on the server

In the admin panel:

1. Enter the public server IP or domain.
2. Confirm the VPN UDP port. The default is `51820`.
3. Click `Save settings`.
4. Click `Build APK on server`.
5. Download the latest APK after the build finishes.

Before each build, the server rewrites:

```text
app/src/main/assets/default_vpn_config.json
```

The downloaded APK will start with server host, port, client virtual IP, DNS, MTU, and shared key already filled in.

## Operations

View logs:

```bash
sudo journalctl -u solovpn -f
```

Restart:

```bash
sudo systemctl restart solovpn
```

This is a custom VPN prototype, not a mature commercial VPN. It does not yet include key rotation, multi-client allocation, replay windows, roaming policy, or full auditing.
