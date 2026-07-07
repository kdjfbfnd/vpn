package com.example.myapplica.vpn

import android.content.Context

object VpnConfigStore {
    private const val PREFS = "solo_vpn"
    private const val KEY_CONFIG = "config"

    fun load(context: Context): VpnConfig {
        val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        val saved = prefs.getString(KEY_CONFIG, null)
        if (!saved.isNullOrBlank()) {
            return VpnConfig.fromJson(saved)
        }
        error("请先登录并获取服务端配置")
    }

    fun save(context: Context, config: VpnConfig) {
        config.requireValid()
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            .edit()
            .putString(KEY_CONFIG, config.toJson())
            .apply()
    }
}
