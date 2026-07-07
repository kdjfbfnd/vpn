package com.example.myapplica.vpn

import android.content.Context
import org.json.JSONObject

data class AuthSession(
    val username: String,
    val token: String,
    val remainingMinutes: Int
)

object AuthStore {
    private const val PREFS = "solo_auth"
    private const val KEY_API_BASE_URL = "api_base_url"
    private const val KEY_USERNAME = "username"
    private const val KEY_TOKEN = "token"
    private const val KEY_REMAINING_MINUTES = "remaining_minutes"
    private const val DEFAULT_ASSET = "default_vpn_config.json"

    fun apiBaseUrl(context: Context): String {
        val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        val assetUrl = assetApiBaseUrl(context)
        val saved = prefs.getString(KEY_API_BASE_URL, null)
        if (!saved.isNullOrBlank() && saved == assetUrl) return saved

        prefs.edit().putString(KEY_API_BASE_URL, assetUrl).apply()
        return assetUrl
    }

    private fun assetApiBaseUrl(context: Context): String {
        val raw = context.assets.open(DEFAULT_ASSET).bufferedReader().use { it.readText() }
        return JSONObject(raw)
            .optString("apiBaseUrl", "http://154.64.230.145:8080")
            .trim()
            .trimEnd('/')
    }

    fun session(context: Context): AuthSession? {
        val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        val username = prefs.getString(KEY_USERNAME, null)
        val token = prefs.getString(KEY_TOKEN, null)
        if (username.isNullOrBlank() || token.isNullOrBlank()) return null
        return AuthSession(username, token, prefs.getInt(KEY_REMAINING_MINUTES, 0))
    }

    fun saveSession(context: Context, session: AuthSession) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            .edit()
            .putString(KEY_USERNAME, session.username)
            .putString(KEY_TOKEN, session.token)
            .putInt(KEY_REMAINING_MINUTES, session.remainingMinutes)
            .apply()
    }

    fun updateMinutes(context: Context, remainingMinutes: Int) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            .edit()
            .putInt(KEY_REMAINING_MINUTES, remainingMinutes)
            .apply()
    }

    fun clear(context: Context) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            .edit()
            .remove(KEY_USERNAME)
            .remove(KEY_TOKEN)
            .remove(KEY_REMAINING_MINUTES)
            .apply()
    }
}
