package com.example.myapplica.vpn

import java.net.InetAddress

object IpPacket {
    fun isIpv4(packet: ByteArray, length: Int): Boolean {
        return length >= 20 && packet[0].toInt().ushr(4) == 4
    }

    fun sourceAddress(packet: ByteArray): InetAddress {
        return InetAddress.getByAddress(packet.copyOfRange(12, 16))
    }

    fun destinationAddress(packet: ByteArray): InetAddress {
        return InetAddress.getByAddress(packet.copyOfRange(16, 20))
    }
}
