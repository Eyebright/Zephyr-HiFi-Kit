package hk.zephyr.terminal

import org.json.JSONArray
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
    private external fun nativeDiscoverServer(): String
    private external fun nativeState(): String
    private external fun nativePause(): String
    private external fun nativeQueueNext(): String
    private external fun nativeQueuePrev(): String
    private external fun nativeStopPlayback(): String
    private external fun nativeSeek(pts: Long): String
    private external fun nativeVolume(v: Int): String
    private external fun nativeUsbProbe(devHex: String, cfgHex: String, product: String): String
    private external fun nativeUsbRates(rangeHex: String): String
    private external fun nativeUsbPlan(devHex: String, cfgHex: String, obsJSON: String): String
    private external fun nativeUsbValidate(iface: Int, alt: Int, replyHex: String): String
    private external fun nativeUsbToneStart(reqJSON: String): String
    private external fun nativeUsbToneStat(): String
    private external fun nativeUsbToneStop(): String

    /** Starts the core; returns the console URL, or null on error. */
    fun start(cfg: JSONObject): String? {
        val res = runCatching { JSONObject(nativeStart(cfg.toString())) }.getOrNull() ?: return null
        return res.optString("console_url").takeIf { it.isNotEmpty() }
    }

    fun stop() = runCatching { nativeStop() }

    /** Scans the LAN for the management server; null when none answers. */
    fun discoverServer(): String? = runCatching {
        JSONObject(nativeDiscoverServer()).optString("server_url").takeIf { it.isNotEmpty() }
    }.getOrNull()

    fun state(): JSONObject? = runCatching { JSONObject(nativeState()) }.getOrNull()

    fun pauseResult(): JSONObject? = runCatching { JSONObject(nativePause()) }.getOrNull()

    fun pause(): Boolean = pauseResult()?.optBoolean("paused") ?: false

    fun queueNext(): JSONObject? = runCatching { JSONObject(nativeQueueNext()) }.getOrNull()

    fun queuePrev(): JSONObject? = runCatching { JSONObject(nativeQueuePrev()) }.getOrNull()

    fun stopPlayback() { runCatching { nativeStopPlayback() } }

    fun seek(pts: Long) { runCatching { nativeSeek(pts) } }

    fun volume(v: Int) { runCatching { nativeVolume(v) } }

    /**
     * USB exclusive output: the descriptor blobs are read by the Java USB host
     * API and decoded by the Go core, which knows the audio class layouts. Every
     * result is either the payload or an "error" key.
     */

    /** Capability model of a DAC: spec, speed, streams, clocks and rate probes. */
    fun usbProbe(devHex: String, cfgHex: String, product: String): JSONObject? =
        runCatching { JSONObject(nativeUsbProbe(devHex, cfgHex, product)) }.getOrNull()

    /** Sample rates a UAC2 clock reported for its GET_RANGE reply. */
    fun usbRates(rangeHex: String): JSONObject? =
        runCatching { JSONObject(nativeUsbRates(rangeHex)) }.getOrNull()

    /** Transfers and packet sizing for one exclusive session at the given format. */
    fun usbPlan(devHex: String, cfgHex: String, observation: JSONObject): JSONObject? =
        runCatching { JSONObject(nativeUsbPlan(devHex, cfgHex, observation.toString())) }.getOrNull()

    /** Request that asks whether an alternate setting is usable; with replyHex it
     *  also answers "supported". */
    fun usbValidate(iface: Int, alt: Int, replyHex: String = ""): JSONObject? =
        runCatching { JSONObject(nativeUsbValidate(iface, alt, replyHex)) }.getOrNull()

    /** Exclusive-output prototype: after claimInterface and setInterface, hand
     *  over the file descriptor and the negotiated format and let the Go core
     *  pump isochronous URBs straight to the DAC.
     *
     *  fd is UsbDeviceConnection.getFileDescriptor(); endpoint is the
     *  bEndpointAddress of the isochronous OUT; subslot is the sample width in
     *  bytes the alternate setting carries. durationMs 0 runs until usbToneStop. */
    fun usbToneStart(fd: Int, endpoint: Int, rate: Int, channels: Int, subslot: Int,
                     durationMs: Int = 0, tones: List<Double> = emptyList(),
                     amplitude: Double = 0.0): JSONObject? {
        val req = JSONObject()
            .put("fd", fd).put("endpoint", endpoint).put("rate", rate)
            .put("channels", channels).put("subslot", subslot)
            .put("duration_ms", durationMs).put("amplitude", amplitude)
            .put("tones", JSONArray(tones))
        return runCatching { JSONObject(nativeUsbToneStart(req.toString())) }.getOrNull()
    }

    /** Live counters of the prototype run: packets, bytes, failures and drift. */
    fun usbToneStat(): JSONObject? =
        runCatching { JSONObject(nativeUsbToneStat()) }.getOrNull()

    /** Ends the run and returns the verdict. Release the interface afterwards:
     *  that rebinds the kernel driver and puts audio back on AAudio. */
    fun usbToneStop(): JSONObject? =
        runCatching { JSONObject(nativeUsbToneStop()) }.getOrNull()
}