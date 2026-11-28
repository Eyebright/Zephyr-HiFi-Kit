package hk.zephyr.terminal

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import android.text.Editable
import android.text.TextWatcher
import android.util.TypedValue
import android.view.Gravity
import android.view.KeyEvent
import android.view.View
import android.view.inputmethod.InputMethodManager
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ProgressBar
import android.widget.TextView
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import org.json.JSONObject
import java.util.concurrent.Executors

/**
 * Setup + console shell. Setup auto-discovers the server on the LAN and shows
 * its address; the user only types the 6-8 digit pairing code into segmented
 * boxes. The activity hands the config to the Go core, starts the foreground
 * playback service and shows the loopback console in a WebView. Configuration
 * is persisted so the terminal reconnects automatically on subsequent launches.
 */
class MainActivity : AppCompatActivity() {

    private lateinit var setup: View
    private lateinit var topbar: View
    private lateinit var web: WebView
    private lateinit var status: TextView
    private lateinit var mediaControls: PlaybackMediaSession
    private lateinit var codeInput: EditText
    private lateinit var codeBoxes: List<TextView>
    private val io = Executors.newSingleThreadExecutor()
    @Volatile private var discovering = false
    private var discoveredServer: String? = null
    private var pendingToken: String? = null

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
        mediaControls = PlaybackMediaSession(this) { message ->
            Toast.makeText(this, message, Toast.LENGTH_SHORT).show()
        }

        findViewById<Button>(R.id.start).setOnClickListener { startFromForm() }
        findViewById<Button>(R.id.reconfigure).setOnClickListener { reconfigure() }
        findViewById<Button>(R.id.rescan).setOnClickListener { startScan() }
        findViewById<Button>(R.id.usb_test_setup).setOnClickListener { openUsbTest() }
        findViewById<Button>(R.id.usb_test_console).setOnClickListener { openUsbTest() }

        codeInput = findViewById(R.id.code_input)
        val boxRow = findViewById<LinearLayout>(R.id.code_boxes)
        val margin = (3 * resources.displayMetrics.density).toInt()
        codeBoxes = (0 until 8).map {
            TextView(this).apply {
                layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.MATCH_PARENT, 1f).apply {
                    setMargins(margin, 0, margin, 0)
                }
                gravity = Gravity.CENTER
                setTextSize(TypedValue.COMPLEX_UNIT_SP, 20f)
                setTextColor(0xFFE8ECFF.toInt())
                setBackgroundResource(R.drawable.code_box_idle)
            }.also(boxRow::addView)
        }
        renderCodeBoxes("")
        codeInput.addTextChangedListener(object : TextWatcher {
            override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
            override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}
            override fun afterTextChanged(s: Editable?) = renderCodeBoxes(s?.toString() ?: "")
        })
        findViewById<View>(R.id.code_entry).setOnClickListener { showCodeKeyboard() }

        if (Build.VERSION.SDK_INT >= 33 &&
            ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            notifPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
        }

        val prefs = getSharedPreferences(PREFS, MODE_PRIVATE)
        val saved = prefs.getString(KEY_CONFIG, null)
        if (saved != null) {
            runCatching { launch(JSONObject(saved)) }
                .onFailure { showSetup(); startScan() }
        } else {
            startScan()
        }
    }

    private fun renderCodeBoxes(text: String) {
        codeBoxes.forEachIndexed { i, box ->
            box.text = text.getOrNull(i)?.toString() ?: ""
            box.setBackgroundResource(when {
                i < text.length -> R.drawable.code_box_filled
                i == text.length -> R.drawable.code_box_active
                else -> R.drawable.code_box_idle
            })
        }
    }

    private fun showCodeKeyboard() {
        codeInput.requestFocus()
        val imm = getSystemService(InputMethodManager::class.java)
        codeInput.post { imm?.showSoftInput(codeInput, InputMethodManager.SHOW_IMPLICIT) }
    }

    private fun startScan() {
        if (discovering) return
        discovering = true
        findViewById<Button>(R.id.rescan).visibility = View.GONE
        findViewById<ProgressBar>(R.id.scan_progress).visibility = View.VISIBLE
        findViewById<TextView>(R.id.scan_state).text = getString(R.string.discovering)
        io.execute {
            val found = ZatCore.discoverServer()
            runOnUiThread {
                discovering = false
                if (setup.visibility != View.VISIBLE) return@runOnUiThread
                findViewById<ProgressBar>(R.id.scan_progress).visibility = View.GONE
                findViewById<Button>(R.id.rescan).visibility = View.VISIBLE
                discoveredServer = found
                val stateView = findViewById<TextView>(R.id.scan_state)
                if (found == null) {
                    stateView.text = getString(R.string.err_discover)
                } else {
                    stateView.text = getString(R.string.scan_found, found)
                    val token = pendingToken
                    if (token != null) {
                        pendingToken = null
                        launchWith(found, token)
                    }
                }
            }
        }
    }

    private fun startFromForm() {
        if (discovering && pendingToken != null) return
        val token = codeInput.text.toString().trim()
        if (token.length !in 6..8 || token.any { !it.isDigit() }) {
            status.text = getString(R.string.err_code)
            showCodeKeyboard()
            return
        }
        status.text = ""
        val manual = findViewById<EditText>(R.id.server).text.toString().trim()
        val server = manual.ifEmpty { discoveredServer }
        if (server != null) {
            launchWith(server, token)
            return
        }
        pendingToken = token
        status.text = getString(R.string.discovering)
        startScan()
    }

    private fun launchWith(server: String, token: String) {
        val cfg = baseConfig().apply {
            put("server_url", server)
            put("token", token)
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
        mediaControls.start()
    }

    override fun dispatchKeyEvent(event: KeyEvent): Boolean =
        mediaControls.handleKeyEvent(event) || super.dispatchKeyEvent(event)

    override fun onDestroy() {
        io.shutdownNow()
        mediaControls.release()
        super.onDestroy()
    }

    private fun openUsbTest() {
        startActivity(Intent(this, UsbTestActivity::class.java))
    }

    private fun reconfigure() {
        pendingToken = null
        discoveredServer = null
        codeInput.setText("")
        findViewById<EditText>(R.id.server).setText("")
        status.text = ""
        mediaControls.stop()
        ZatCore.stop()
        stopService(Intent(this, ZatService::class.java))
        getSharedPreferences(PREFS, MODE_PRIVATE).edit().remove(KEY_CONFIG).apply()
        web.visibility = View.GONE
        web.stopLoading()
        showSetup()
        startScan()
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