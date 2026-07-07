package com.example.myapplica.vpn

import org.json.JSONObject

data class VpnConfig(
    val profileName: String,
    val serverHost: String,
    val serverPort: Int,
    val clientAddress: String,
    val clientPrefixLength: Int,
    val dns: String,
    val mtu: Int,
    val sharedKey: String
) {
    fun toJson(): String {
        return JSONObject()
            .put("profileName", profileName)
            .put("serverHost", serverHost)
            .put("serverPort", serverPort)
            .put("clientAddress", clientAddress)
            .put("clientPrefixLength", clientPrefixLength)
            .put("dns", dns)
            .put("mtu", mtu)
            .put("sharedKey", sharedKey)
            .toString(2)
    }

    companion object {
        fun fromJson(raw: String): VpnConfig {
            val json = JSONObject(raw)
            return VpnConfig(
                profileName = json.optString("profileName", "Solo VPN"),
                serverHost = json.getString("serverHost").trim(),
                serverPort = json.optInt("serverPort", 51820),
                clientAddress = json.getString("clientAddress").trim(),
                clientPrefixLength = json.optInt("clientPrefixLength", 32),
                dns = json.optString("dns", "1.1.1.1").trim(),
                mtu = json.optInt("mtu", 1280),
                sharedKey = json.getString("sharedKey").trim()
            ).also { it.requireValid() }
        }
    }

    fun requireValid() {
        require(serverHost.isNotBlank()) { "Server host is required" }
        require(serverPort in 1..65535) { "Server port must be 1-65535" }
        require(clientPrefixLength in 1..32) { "Client prefix length must be 1-32" }
        require(mtu in 576..1500) { "MTU must be 576-1500" }
        require(sharedKey.isNotBlank()) { "Shared key is required" }
        val decoded = android.util.Base64.decode(sharedKey, android.util.Base64.DEFAULT)
        require(decoded.size == 32) { "Shared key must be 32 bytes Base64" }
    }
}
