package hk.zephyr.terminal

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import android.view.View
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import org.json.JSONObject

/**
 * Setup + console shell. The user enters the server URL and a one-time token; the
 * activity hands the config to the Go core, starts the foreground playback service
 * and shows the loopback console in a WebView. Configuration is persisted so the
 * terminal reconnects automatically on subsequent launches.
 */
class MainActivity : AppCompatActivity() {

    private lateinit var setup: View
    private lateinit var topbar: View
    private lateinit var web: WebView
    private lateinit var status: TextView

    private val notifPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)
        setup = findViewById(R.id.setup)
        topbar = findViewById(R.id.topbar)
        web = findViewById(R.id.webview)
        status = findViewById(R.id.status)
        web.settings.javaScriptEnabled = true
        web.settings.domStorageEnabled = true
        web.webViewClient = WebViewClient()

        findViewById<Button>(R.id.start).setOnClickListener { startFromForm() }
        findViewById<Button>(R.id.reconfigure).setOnClickListener { reconfigure() }

        if (Build.VERSION.SDK_INT >= 33 &&
            ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            notifPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
        }

        val prefs = getSharedPreferences(PREFS, MODE_PRIVATE)
        val saved = prefs.getString(KEY_CONFIG, null)
        if (saved != null) {
            runCatching { launch(JSONObject(saved)) }
                .onFailure { showSetup() }
        }
    }

    private fun startFromForm() {
        val server = findViewById<EditText>(R.id.server).text.toString().trim()
        val token = findViewById<EditText>(R.id.token).text.toString().trim()
        val alias = findViewById<EditText>(R.id.alias).text.toString().trim()
        if (server.isEmpty()) {
            status.text = getString(R.string.err_server)
            return
        }
        val cfg = baseConfig().apply {
            put("server_url", server)
            put("token", token)
            put("alias", alias)
        }
        getSharedPreferences(PREFS, MODE_PRIVATE).edit().putString(KEY_CONFIG, cfg.toString()).apply()
        launch(cfg)
    }

    private fun baseConfig(): JSONObject = JSONObject().apply {
        put("data_dir", filesDir.absolutePath)
        put("android_id", Settings.Secure.getString(contentResolver, Settings.Secure.ANDROID_ID) ?: "")
        put("os_version", "Android " + Build.VERSION.RELEASE)
        put("listen_addr", "127.0.0.1:8090")
    }

    private fun launch(cfg: JSONObject) {
        val url = ZatCore.start(cfg)
        if (url == null) {
            status.text = getString(R.string.err_start)
            showSetup()
            return
        }
        ContextCompat.startForegroundService(this, Intent(this, ZatService::class.java))
        setup.visibility = View.GONE
        topbar.visibility = View.VISIBLE
        web.visibility = View.VISIBLE
        web.loadUrl(url)
    }

    private fun reconfigure() {
        ZatCore.stop()
        stopService(Intent(this, ZatService::class.java))
        getSharedPreferences(PREFS, MODE_PRIVATE).edit().remove(KEY_CONFIG).apply()
        web.visibility = View.GONE
        web.stopLoading()
        showSetup()
    }

    private fun showSetup() {
        topbar.visibility = View.GONE
        web.visibility = View.GONE
        setup.visibility = View.VISIBLE
    }

    companion object {
        const val PREFS = "zat"
        const val KEY_CONFIG = "config"
    }
}