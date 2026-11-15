package hk.zephyr.terminal

import org.json.JSONObject

/**
 * JNI bridge to the Go c-shared terminal core (libzatagent.so + libzatjni.so).
 * The Go side owns the ZAT sink, AAudio output, registration and the loopback
 * console; Kotlin only starts/stops it and reads state for the native UI.
 */
object ZatCore {
    init {
        System.loadLibrary("zatagent")
        System.loadLibrary("zatjni")
    }

    private external fun nativeStart(cfg: String): String
    private external fun nativeStop()
    private external fun nativeState(): String
    private external fun nativePause(): String
    private external fun nativeStopPlayback(): String
    private external fun nativeSeek(pts: Long): String
    private external fun nativeVolume(v: Int): String

    /** Starts the core; returns the console URL, or null on error. */
    fun start(cfg: JSONObject): String? {
        val res = runCatching { JSONObject(nativeStart(cfg.toString())) }.getOrNull() ?: return null
        return res.optString("console_url").takeIf { it.isNotEmpty() }
    }

    fun stop() = runCatching { nativeStop() }

    fun state(): JSONObject? = runCatching { JSONObject(nativeState()) }.getOrNull()

    fun pause(): Boolean = runCatching { JSONObject(nativePause()).optBoolean("paused") }.getOrDefault(false)

    fun stopPlayback() { runCatching { nativeStopPlayback() } }

    fun seek(pts: Long) { runCatching { nativeSeek(pts) } }

    fun volume(v: Int) { runCatching { nativeVolume(v) } }
}