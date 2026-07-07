package com.example.myapplica.vpn

import android.content.Context
import org.json.JSONException
import org.json.JSONObject
import java.io.BufferedReader
import java.io.InputStreamReader
import java.io.OutputStreamWriter
import java.net.HttpURLConnection
import java.net.URL

data class ConfigResult(
    val config: VpnConfig,
    val remainingMinutes: Int
)

class ApiException(
    val statusCode: Int,
    message: String
) : Exception(message)

object AuthApi {
    fun register(context: Context, username: String, password: String): AuthSession {
        return auth(context, "/api/register", username, password)
    }

    fun login(context: Context, username: String, password: String): AuthSession {
        return auth(context, "/api/login", username, password)
    }

    fun me(context: Context): AuthSession {
        val session = AuthStore.session(context) ?: error("请先登录")
        val json = request(context, "GET", "/api/me", null, session)
        return sessionFromJson(json)
    }

    fun fetchConfig(context: Context): ConfigResult {
        val session = AuthStore.session(context) ?: error("请先登录")
        val json = request(context, "GET", "/api/config", null, session)
        val config = VpnConfig.fromJson(json.toString())
        return ConfigResult(config, json.optInt("remainingMinutes", 0))
    }

    fun tick(context: Context): AuthSession {
        val session = AuthStore.session(context) ?: error("请先登录")
        val json = request(context, "POST", "/api/tick", JSONObject(), session)
        return sessionFromJson(json)
    }

    private fun auth(context: Context, path: String, username: String, password: String): AuthSession {
        val body = JSONObject()
            .put("username", username.trim())
            .put("password", password)
        return sessionFromJson(request(context, "POST", path, body, null))
    }

    private fun sessionFromJson(json: JSONObject): AuthSession {
        return AuthSession(
            username = json.getString("username"),
            token = json.optString("token"),
            remainingMinutes = json.optInt("remainingMinutes", 0)
        )
    }

    private fun request(
        context: Context,
        method: String,
        path: String,
        body: JSONObject?,
        session: AuthSession?
    ): JSONObject {
        val url = URL(AuthStore.apiBaseUrl(context) + path)
        val conn = (url.openConnection() as HttpURLConnection).apply {
            requestMethod = method
            connectTimeout = 10000
            readTimeout = 15000
            setRequestProperty("Accept", "application/json")
            if (session != null) {
                setRequestProperty("Authorization", "Bearer ${session.username}:${session.token}")
            }
            if (body != null) {
                doOutput = true
                setRequestProperty("Content-Type", "application/json; charset=utf-8")
            }
        }
        if (body != null) {
            OutputStreamWriter(conn.outputStream, Charsets.UTF_8).use { it.write(body.toString()) }
        }

        val code = conn.responseCode
        val stream = if (code in 200..299) conn.inputStream else conn.errorStream
        val raw = stream?.use {
            BufferedReader(InputStreamReader(it, Charsets.UTF_8)).readText()
        }.orEmpty()
        val json = try {
            if (raw.isBlank()) JSONObject() else JSONObject(raw)
        } catch (_: JSONException) {
            throw ApiException(code, "服务器返回异常，请检查管理端地址")
        }
        if (code !in 200..299) {
            throw ApiException(code, friendlyError(code, json.optString("error")))
        }
        return json
    }

    private fun friendlyError(code: Int, error: String): String {
        return when (code) {
            HttpURLConnection.HTTP_UNAUTHORIZED -> "登录已失效，请重新登录"
            HttpURLConnection.HTTP_PAYMENT_REQUIRED -> "连接时长不足，请联系管理员充值"
            HttpURLConnection.HTTP_NOT_FOUND -> "服务器接口不存在，请检查管理端地址"
            in 500..599 -> "服务器异常，请稍后重试"
            else -> error.ifBlank { "请求失败（$code）" }
        }
    }
}
