package com.example.myapplica

import android.Manifest
import android.animation.AnimatorSet
import android.animation.ObjectAnimator
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.MotionEvent
import android.view.View
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.app.ActivityCompat
import com.example.myapplica.vpn.ApiException
import com.example.myapplica.vpn.AuthApi
import com.example.myapplica.vpn.AuthSession
import com.example.myapplica.vpn.AuthStore
import com.example.myapplica.vpn.SoloVpnService
import com.example.myapplica.vpn.VpnConfigStore
import java.net.HttpURLConnection
import java.util.concurrent.Executors

class MainActivity : AppCompatActivity() {
    private lateinit var loginCard: View
    private lateinit var connectCard: View
    private lateinit var usernameInput: EditText
    private lateinit var passwordInput: EditText
    private lateinit var statusText: TextView
    private lateinit var accountText: TextView
    private lateinit var timeText: TextView
    private lateinit var loginButton: Button
    private lateinit var registerButton: Button
    private lateinit var startButton: Button
    private lateinit var stopButton: Button
    private lateinit var logoutButton: Button

    private val worker = Executors.newSingleThreadExecutor()
    private val mainHandler = Handler(Looper.getMainLooper())
    private val refreshRunnable = object : Runnable {
        override fun run() {
            if (AuthStore.session(this@MainActivity) != null) {
                refreshAccount()
                mainHandler.postDelayed(this, 30_000)
            }
        }
    }

    private val vpnPermission = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) {
        startVpnService()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        loginCard = findViewById(R.id.loginCard)
        connectCard = findViewById(R.id.connectCard)
        usernameInput = findViewById(R.id.usernameInput)
        passwordInput = findViewById(R.id.passwordInput)
        statusText = findViewById(R.id.statusText)
        accountText = findViewById(R.id.accountText)
        timeText = findViewById(R.id.timeText)
        loginButton = findViewById(R.id.loginButton)
        registerButton = findViewById(R.id.registerButton)
        startButton = findViewById(R.id.startButton)
        stopButton = findViewById(R.id.stopButton)
        logoutButton = findViewById(R.id.logoutButton)

        listOf(loginButton, registerButton, startButton, stopButton, logoutButton).forEach { it.addPressAnimation() }
        loginButton.setOnClickListener { submitAuth(register = false) }
        registerButton.setOnClickListener { submitAuth(register = true) }
        startButton.setOnClickListener { prepareAndStartVpn() }
        stopButton.setOnClickListener { stopVpnService() }
        logoutButton.setOnClickListener { logout() }

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            ActivityCompat.requestPermissions(this, arrayOf(Manifest.permission.POST_NOTIFICATIONS), 10)
        }

        renderSession(AuthStore.session(this))
        animateEntrance()
    }

    override fun onResume() {
        super.onResume()
        mainHandler.removeCallbacks(refreshRunnable)
        if (AuthStore.session(this) != null) {
            refreshAccount()
            mainHandler.postDelayed(refreshRunnable, 30_000)
        }
    }

    override fun onPause() {
        mainHandler.removeCallbacks(refreshRunnable)
        super.onPause()
    }

    override fun onDestroy() {
        worker.shutdownNow()
        super.onDestroy()
    }

    private fun submitAuth(register: Boolean) {
        val username = usernameInput.text.toString().trim()
        val password = passwordInput.text.toString()
        if (username.length < 3 || password.length < 6) {
            toast("账号至少 3 位，密码至少 6 位")
            return
        }

        setAuthButtonsEnabled(false)
        updateStatus(if (register) "正在注册..." else "正在登录...")
        worker.execute {
            try {
                val session = if (register) {
                    AuthApi.register(this, username, password)
                } else {
                    AuthApi.login(this, username, password)
                }
                AuthStore.saveSession(this, session)
                runOnUiThread {
                    passwordInput.setText("")
                    renderSession(session)
                    updateStatus(if (register) "注册成功，当前时长 0 分钟" else "登录成功")
                    toast(if (register) "注册成功" else "登录成功")
                }
            } catch (error: Exception) {
                runOnUiThread {
                    val message = error.message ?: "请求失败"
                    updateStatus(message)
                    toast(message)
                }
            } finally {
                runOnUiThread { setAuthButtonsEnabled(true) }
            }
        }
    }

    private fun refreshAccount() {
        worker.execute {
            try {
                val session = AuthApi.me(this)
                AuthStore.saveSession(this, session)
                runOnUiThread { renderSession(session) }
            } catch (_: Exception) {
            }
        }
    }

    private fun prepareAndStartVpn() {
        val session = AuthStore.session(this)
        if (session == null) {
            renderSession(null)
            toast("请先登录")
            return
        }

        startButton.isEnabled = false
        updateStatus("正在获取连接配置...")
        worker.execute {
            try {
                val result = AuthApi.fetchConfig(this)
                AuthStore.updateMinutes(this, result.remainingMinutes)
                if (result.remainingMinutes <= 0) {
                    runOnUiThread {
                        renderSession(session.copy(remainingMinutes = 0))
                        updateStatus("连接时长不足")
                        toast("连接时长不足，请联系管理员充值")
                    }
                    return@execute
                }
                VpnConfigStore.save(this, result.config)
                runOnUiThread {
                    renderSession(session.copy(remainingMinutes = result.remainingMinutes))
                    val intent = VpnService.prepare(this)
                    if (intent != null) {
                        vpnPermission.launch(intent)
                    } else {
                        startVpnService()
                    }
                }
            } catch (error: Exception) {
                runOnUiThread {
                    val message = vpnStartErrorMessage(error)
                    updateStatus(message)
                    toast(message)
                }
            } finally {
                runOnUiThread { startButton.isEnabled = true }
            }
        }
    }

    private fun vpnStartErrorMessage(error: Exception): String {
        if (error is ApiException && error.statusCode == HttpURLConnection.HTTP_PAYMENT_REQUIRED) {
            return "连接时长不足"
        }
        return error.message?.takeIf { it.isNotBlank() } ?: "获取配置失败"
    }

    private fun startVpnService() {
        val intent = Intent(this, SoloVpnService::class.java)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            startForegroundService(intent)
        } else {
            startService(intent)
        }
        updateStatus("状态：VPN 正在启动")
    }

    private fun stopVpnService() {
        startService(Intent(this, SoloVpnService::class.java).setAction(SoloVpnService.ACTION_STOP))
        updateStatus("状态：已请求停止")
        refreshAccount()
    }

    private fun logout() {
        stopVpnService()
        AuthStore.clear(this)
        renderSession(null)
        updateStatus("请先登录")
    }

    private fun renderSession(session: AuthSession?) {
        if (session == null) {
            loginCard.visibility = View.VISIBLE
            connectCard.visibility = View.GONE
            accountText.text = ""
            timeText.text = ""
            return
        }
        loginCard.visibility = View.GONE
        connectCard.visibility = View.VISIBLE
        accountText.text = "当前账号：${session.username}"
        timeText.text = "剩余 ${session.remainingMinutes} 分钟"
        if (session.remainingMinutes <= 0) {
            updateStatus("连接时长不足")
        }
    }

    private fun setAuthButtonsEnabled(enabled: Boolean) {
        loginButton.isEnabled = enabled
        registerButton.isEnabled = enabled
    }

    private fun updateStatus(message: String) {
        statusText.animate().cancel()
        statusText.text = message
        statusText.alpha = 0.72f
        statusText.scaleX = 0.98f
        statusText.scaleY = 0.98f
        statusText.animate()
            .alpha(1f)
            .scaleX(1f)
            .scaleY(1f)
            .setDuration(220)
            .start()
    }

    private fun animateEntrance() {
        val views = listOf<View>(
            findViewById(R.id.heroCard),
            loginCard,
            connectCard
        ).filter { it.visibility == View.VISIBLE }
        val animators = views.flatMapIndexed { index, view ->
            view.alpha = 0f
            view.translationY = 22f
            listOf(
                ObjectAnimator.ofFloat(view, View.ALPHA, 0f, 1f),
                ObjectAnimator.ofFloat(view, View.TRANSLATION_Y, 22f, 0f)
            ).onEach { it.startDelay = index * 90L }
        }
        AnimatorSet().apply {
            playTogether(animators)
            duration = 360
            start()
        }
    }

    private fun View.addPressAnimation() {
        setOnTouchListener { view, event ->
            when (event.actionMasked) {
                MotionEvent.ACTION_DOWN -> view.animate().scaleX(0.97f).scaleY(0.97f).setDuration(90).start()
                MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> view.animate().scaleX(1f).scaleY(1f).setDuration(120).start()
            }
            false
        }
    }

    private fun toast(message: String) {
        Toast.makeText(this, message, Toast.LENGTH_SHORT).show()
    }
}
