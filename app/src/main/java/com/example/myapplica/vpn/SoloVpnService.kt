package com.example.myapplica.vpn

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import androidx.core.app.NotificationCompat
import com.example.myapplica.MainActivity
import com.example.myapplica.R
import java.io.FileInputStream
import java.io.FileOutputStream
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetSocketAddress
import java.util.concurrent.Executors
import java.util.concurrent.Future
import java.util.concurrent.atomic.AtomicBoolean

class SoloVpnService : VpnService() {
    private val running = AtomicBoolean(false)
    private val executor = Executors.newFixedThreadPool(3)
    private var tun: ParcelFileDescriptor? = null
    private var socket: DatagramSocket? = null
    private var tunToUdp: Future<*>? = null
    private var udpToTun: Future<*>? = null
    private var keepAlive: Future<*>? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            stopTunnel()
            stopSelf()
            return START_NOT_STICKY
        }

        startForeground(NOTIFICATION_ID, buildNotification())
        if (running.compareAndSet(false, true)) {
            executor.execute { startTunnel() }
        }
        return START_STICKY
    }

    override fun onDestroy() {
        stopTunnel()
        executor.shutdownNow()
        super.onDestroy()
    }

    private fun startTunnel() {
        try {
            val config = VpnConfigStore.load(this)
            val key = android.util.Base64.decode(config.sharedKey, android.util.Base64.DEFAULT)
            val codec = SoloPacketCodec(key)

            tun = Builder()
                .setSession(config.profileName)
                .setMtu(config.mtu)
                .addAddress(config.clientAddress, config.clientPrefixLength)
                .addRoute("0.0.0.0", 0)
                .addDnsServer(config.dns)
                .establish()

            val vpnTun = tun ?: error("Cannot create Android VPN interface")
            val udp = DatagramSocket()
            protect(udp)
            udp.connect(InetSocketAddress(config.serverHost, config.serverPort))
            udp.soTimeout = 1000
            socket = udp

            sendControl(codec, udp, "REGISTER ${config.clientAddress}")

            tunToUdp = executor.submit { readTunWriteUdp(vpnTun, udp, codec) }
            udpToTun = executor.submit { readUdpWriteTun(vpnTun, udp, codec) }
            keepAlive = executor.submit { keepAliveLoop(udp, codec, config.clientAddress) }
        } catch (_: Exception) {
            stopTunnel()
            stopSelf()
        }
    }

    private fun readTunWriteUdp(
        vpnTun: ParcelFileDescriptor,
        udp: DatagramSocket,
        codec: SoloPacketCodec
    ) {
        val input = FileInputStream(vpnTun.fileDescriptor)
        val buffer = ByteArray(MAX_PACKET_SIZE)

        while (running.get()) {
            val length = input.read(buffer)
            if (length <= 0 || !IpPacket.isIpv4(buffer, length)) continue

            val sealed = codec.seal(SoloPacketCodec.TYPE_DATA, buffer, length)
            udp.send(DatagramPacket(sealed, sealed.size))
        }
    }

    private fun readUdpWriteTun(
        vpnTun: ParcelFileDescriptor,
        udp: DatagramSocket,
        codec: SoloPacketCodec
    ) {
        val output = FileOutputStream(vpnTun.fileDescriptor)
        val buffer = ByteArray(MAX_PACKET_SIZE + OVERHEAD_SIZE)

        while (running.get()) {
            try {
                val packet = DatagramPacket(buffer, buffer.size)
                udp.receive(packet)
                val decoded = codec.open(packet.data, packet.length) ?: continue
                if (decoded.type == SoloPacketCodec.TYPE_DATA && IpPacket.isIpv4(decoded.payload, decoded.payload.size)) {
                    output.write(decoded.payload)
                }
            } catch (_: java.net.SocketTimeoutException) {
            }
        }
    }

    private fun keepAliveLoop(udp: DatagramSocket, codec: SoloPacketCodec, address: String) {
        var elapsedMs = 0L
        while (running.get()) {
            sendControl(codec, udp, "REGISTER $address")
            Thread.sleep(15_000)
            elapsedMs += 15_000
            if (elapsedMs >= 60_000) {
                elapsedMs = 0
                try {
                    val session = AuthApi.tick(this)
                    AuthStore.updateMinutes(this, session.remainingMinutes)
                    if (session.remainingMinutes <= 0) {
                        stopTunnel()
                        stopSelf()
                        return
                    }
                } catch (_: Exception) {
                }
            }
        }
    }

    private fun sendControl(codec: SoloPacketCodec, udp: DatagramSocket, message: String) {
        val sealed = codec.seal(SoloPacketCodec.TYPE_CONTROL, message.toByteArray(Charsets.UTF_8))
        udp.send(DatagramPacket(sealed, sealed.size))
    }

    private fun stopTunnel() {
        running.set(false)
        tunToUdp?.cancel(true)
        udpToTun?.cancel(true)
        keepAlive?.cancel(true)
        socket?.close()
        tun?.close()
        socket = null
        tun = null
    }

    private fun buildNotification(): Notification {
        createNotificationChannel()
        val openIntent = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        )
        val stopIntent = PendingIntent.getService(
            this,
            1,
            Intent(this, SoloVpnService::class.java).setAction(ACTION_STOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        )

        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.mipmap.ic_launcher)
            .setContentTitle("极速传 VPN 正在运行")
            .setContentText("正在通过加密隧道转发流量")
            .setContentIntent(openIntent)
            .setOngoing(true)
            .addAction(0, "停止", stopIntent)
            .build()
    }

    private fun createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val manager = getSystemService(NotificationManager::class.java)
            manager.createNotificationChannel(
                NotificationChannel(CHANNEL_ID, "Solo VPN", NotificationManager.IMPORTANCE_LOW)
            )
        }
    }

    companion object {
        const val ACTION_STOP = "com.example.myapplica.vpn.STOP"
        private const val CHANNEL_ID = "solo_vpn"
        private const val NOTIFICATION_ID = 2001
        private const val MAX_PACKET_SIZE = 32767
        private const val OVERHEAD_SIZE = 128
    }
}
