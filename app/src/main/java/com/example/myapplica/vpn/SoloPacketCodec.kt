package com.example.myapplica.vpn

import java.nio.ByteBuffer
import java.security.SecureRandom
import java.util.concurrent.atomic.AtomicLong
import javax.crypto.Cipher
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

class SoloPacketCodec(private val key: ByteArray) {
    private val secureRandom = SecureRandom()
    private val sessionPrefix = ByteArray(4).also { secureRandom.nextBytes(it) }
    private val sequence = AtomicLong(secureRandom.nextInt().toLong() and 0xffffffffL)
    private val secretKey = SecretKeySpec(key, "AES")

    fun seal(type: Byte, payload: ByteArray, length: Int = payload.size): ByteArray {
        val seq = sequence.incrementAndGet()
        val nonce = ByteArray(NONCE_SIZE)
        System.arraycopy(sessionPrefix, 0, nonce, 0, sessionPrefix.size)
        ByteBuffer.wrap(nonce, 4, 8).putLong(seq)

        val aad = ByteBuffer.allocate(AAD_SIZE)
            .putInt(MAGIC)
            .put(type)
            .putLong(seq)
            .array()

        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, secretKey, GCMParameterSpec(TAG_BITS, nonce))
        cipher.updateAAD(aad)
        val encrypted = cipher.doFinal(payload, 0, length)

        return ByteBuffer.allocate(AAD_SIZE + NONCE_SIZE + encrypted.size)
            .put(aad)
            .put(nonce)
            .put(encrypted)
            .array()
    }

    fun open(packet: ByteArray, length: Int): DecodedPacket? {
        if (length <= AAD_SIZE + NONCE_SIZE + TAG_SIZE) return null

        val buffer = ByteBuffer.wrap(packet, 0, length)
        val magic = buffer.int
        if (magic != MAGIC) return null

        val type = buffer.get()
        val seq = buffer.long
        val nonce = ByteArray(NONCE_SIZE)
        buffer.get(nonce)
        val encrypted = ByteArray(buffer.remaining())
        buffer.get(encrypted)

        val aad = ByteBuffer.allocate(AAD_SIZE)
            .putInt(MAGIC)
            .put(type)
            .putLong(seq)
            .array()

        return try {
            val cipher = Cipher.getInstance("AES/GCM/NoPadding")
            cipher.init(Cipher.DECRYPT_MODE, secretKey, GCMParameterSpec(TAG_BITS, nonce))
            cipher.updateAAD(aad)
            DecodedPacket(type, cipher.doFinal(encrypted))
        } catch (_: Exception) {
            null
        }
    }

    data class DecodedPacket(val type: Byte, val payload: ByteArray)

    companion object {
        const val TYPE_CONTROL: Byte = 1
        const val TYPE_DATA: Byte = 2
        private const val MAGIC = 0x53565031
        private const val AAD_SIZE = 13
        private const val NONCE_SIZE = 12
        private const val TAG_SIZE = 16
        private const val TAG_BITS = 128
    }
}
