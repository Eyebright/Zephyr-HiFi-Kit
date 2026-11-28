package hk.zephyr.terminal

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Intent
import android.content.IntentFilter
import android.content.res.ColorStateList
import android.hardware.usb.UsbDevice
import android.hardware.usb.UsbDeviceConnection
import android.hardware.usb.UsbInterface
import android.hardware.usb.UsbManager
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.graphics.Typeface
import android.view.View
import android.widget.AdapterView
import android.widget.ArrayAdapter
import android.widget.Button
import android.widget.ProgressBar
import android.widget.ScrollView
import android.widget.Spinner
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import org.json.JSONArray
import org.json.JSONObject
import java.io.IOException
import java.util.Locale
import java.util.concurrent.Executors

class UsbTestActivity : AppCompatActivity() {

    private companion object {
        const val USB_PERMISSION_ACTION = "android.hardware.usb.action.USB_PERMISSION"
    }

    private data class FormatOption(
        val alt: Int,
        val bits: Int,
        val subslot: Int,
        val channels: Int,
        val rates: List<Int>
    )

    private data class StreamOption(
        val interfaceNumber: Int,
        val endpoint: JSONObject,
        val formats: List<FormatOption>,
        val rates: List<Int>,
        val rangeHex: String = "",
        val rateError: String? = null
    )

    private data class Target(
        val device: UsbDevice,
        val deviceHex: String,
        val configHex: String,
        val probe: JSONObject,
        val streams: List<StreamOption>
    )

    private data class TestRequest(
        val target: Target,
        val stream: StreamOption,
        val alt: Int,
        val rate: Int,
        val bits: Int,
        val channels: Int,
        val durationMS: Int
    )

    private class TestCancelled : Exception()

    private val usbManager: UsbManager? by lazy { getSystemService(UsbManager::class.java) }
    private val executor = Executors.newSingleThreadExecutor()
    private val handler = Handler(Looper.getMainLooper())

    private lateinit var devices: Spinner
    private lateinit var refresh: Button
    private lateinit var probe: Button
    private lateinit var permission: TextView
    private lateinit var progress: ProgressBar
    private lateinit var capabilities: TextView
    private lateinit var streams: Spinner
    private lateinit var rates: Spinner
    private lateinit var bits: Spinner
    private lateinit var channels: Spinner
    private lateinit var duration: Spinner
    private lateinit var asyncWarning: TextView
    private lateinit var run: Button
    private lateinit var stop: Button
    private lateinit var verdict: TextView
    private lateinit var transport: TextView
    private lateinit var stats: TextView
    private lateinit var logScroll: ScrollView
    private lateinit var logView: TextView

    private var deviceList: List<UsbDevice> = emptyList()
    private var displayTarget: Target? = null
    private var rateValues: List<Int> = emptyList()
    private var bitValues: List<Int> = emptyList()
    private var channelValues: List<Int> = emptyList()
    private val durationValues = listOf(2000, 5000, 10000)
    private val logLines = ArrayList<String>()
    private var updatingSpinners = false
    private var busy = false
    private var running = false
    private var pendingPermissionDevice: UsbDevice? = null

    private var activeTarget: Target? = null
    private var connection: UsbDeviceConnection? = null
    private var claimedInterface: UsbInterface? = null
    private var resumeAfterTest = false
    private var toneRunning = false

    @Volatile private var stopRequested = false
    @Volatile private var destroyed = false

    private val usbReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: android.content.Context, intent: Intent) {
            when (intent.action) {
                USB_PERMISSION_ACTION -> handlePermission(intent)
                UsbManager.ACTION_USB_DEVICE_ATTACHED -> refreshDevices()
                UsbManager.ACTION_USB_DEVICE_DETACHED -> handleDetach(intent)
            }
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_usb_test)
        devices = findViewById(R.id.usb_devices)
        refresh = findViewById(R.id.usb_refresh)
        probe = findViewById(R.id.usb_probe)
        permission = findViewById(R.id.usb_permission)
        progress = findViewById(R.id.usb_progress)
        capabilities = findViewById(R.id.usb_capabilities)
        streams = findViewById(R.id.usb_streams)
        rates = findViewById(R.id.usb_rates)
        bits = findViewById(R.id.usb_bits)
        channels = findViewById(R.id.usb_channels)
        duration = findViewById(R.id.usb_duration)
        asyncWarning = findViewById(R.id.usb_async_warning)
        run = findViewById(R.id.usb_run)
        stop = findViewById(R.id.usb_stop)
        verdict = findViewById(R.id.usb_verdict)
        transport = findViewById(R.id.usb_transport)
        stats = findViewById(R.id.usb_stats)
        logScroll = findViewById(R.id.usb_log_scroll)
        logView = findViewById(R.id.usb_log)
        stats.typeface = Typeface.MONOSPACE
        logView.typeface = Typeface.MONOSPACE

        findViewById<Button>(R.id.usb_back).setOnClickListener { finish() }
        refresh.setOnClickListener { refreshDevices() }
        probe.setOnClickListener { probeSelected() }
        run.setOnClickListener { startSelectedTest() }
        stop.setOnClickListener { requestStop() }

        devices.onItemSelectedListener = object : AdapterView.OnItemSelectedListener {
            override fun onItemSelected(parent: AdapterView<*>?, view: View?, position: Int, id: Long) {
                if (updatingSpinners) return
                val selected = selectedDevice()
                if (displayTarget?.device?.deviceName != selected?.deviceName) clearTargetUi(true)
                probe.isEnabled = !busy && selected != null
            }

            override fun onNothingSelected(parent: AdapterView<*>?) = Unit
        }
        val formatListener = object : AdapterView.OnItemSelectedListener {
            override fun onItemSelected(parent: AdapterView<*>?, view: View?, position: Int, id: Long) {
                if (updatingSpinners) return
                when (parent?.id) {
                    R.id.usb_streams -> refreshRateOptions()
                    R.id.usb_rates -> refreshBitOptions()
                    R.id.usb_bits -> refreshChannelOptions()
                }
                run.isEnabled = !busy && !running && currentFormat() != null
            }

            override fun onNothingSelected(parent: AdapterView<*>?) = Unit
        }
        streams.onItemSelectedListener = formatListener
        rates.onItemSelectedListener = formatListener
        bits.onItemSelectedListener = formatListener
        channels.onItemSelectedListener = formatListener
        bindSpinner(duration, durationValues.map { getString(R.string.usb_duration_value, it / 1000) })

        val filter = IntentFilter(USB_PERMISSION_ACTION).apply {
            addAction(UsbManager.ACTION_USB_DEVICE_ATTACHED)
            addAction(UsbManager.ACTION_USB_DEVICE_DETACHED)
        }
        ContextCompat.registerReceiver(this, usbReceiver, filter, ContextCompat.RECEIVER_NOT_EXPORTED)
        transport.text = getString(R.string.usb_output_aaudio)
        showVerdict(getString(R.string.usb_verdict_idle), R.color.usb_idle)
        refreshDevices()
        setBusy(false)
    }

    override fun onStop() {
        super.onStop()
        if (!isChangingConfigurations && pendingPermissionDevice == null) {
            requestStop()
        }
    }

    override fun onDestroy() {
        destroyed = true
        stopRequested = true
        handler.removeCallbacksAndMessages(null)
        runCatching { unregisterReceiver(usbReceiver) }
        executor.execute {
            runCatching { if (toneRunning) ZatCore.usbToneStop() }
            toneRunning = false
            claimedInterface?.let { iface -> runCatching { connection?.releaseInterface(iface) } }
            claimedInterface = null
            runCatching { connection?.close() }
            connection = null
            activeTarget = null
        }
        executor.shutdown()
        super.onDestroy()
    }

    private fun refreshDevices() {
        val manager = usbManager
        if (manager == null) {
            deviceList = emptyList()
            bindSpinner(devices, listOf(getString(R.string.usb_no_usb_host)))
            permission.text = getString(R.string.usb_no_usb_host)
            probe.isEnabled = false
            return
        }
        val current = selectedDevice()?.deviceName
        deviceList = manager.deviceList.values.sortedWith(
            compareBy({ it.productName ?: it.manufacturerName ?: "USB DAC" }, { it.deviceName })
        )
        val labels = deviceList.map { deviceLabel(it) }.ifEmpty { listOf(getString(R.string.usb_no_devices)) }
        bindSpinner(devices, labels, current?.let { name -> deviceLabel(deviceList.firstOrNull { it.deviceName == name } ?: return@let null) })
        if (displayTarget != null && deviceList.none { it.deviceName == displayTarget?.device?.deviceName }) {
            clearTargetUi(!running)
        }
        probe.isEnabled = !busy && selectedDevice() != null
    }

    private fun deviceLabel(device: UsbDevice): String {
        val name = device.productName ?: device.manufacturerName ?: getString(R.string.usb_device_fallback)
        return getString(R.string.usb_device_label, name, device.vendorId, device.productId)
    }

    private fun selectedDevice(): UsbDevice? = deviceList.getOrNull(devices.selectedItemPosition)

    private fun handlePermission(intent: Intent) {
        val pending = pendingPermissionDevice ?: return
        val responded = intent.getParcelableExtra<UsbDevice>(UsbManager.EXTRA_DEVICE)
        if (responded != null && responded.deviceName != pending.deviceName) return
        val granted = intent.getBooleanExtra(UsbManager.EXTRA_PERMISSION_GRANTED, false) &&
            usbManager?.hasPermission(pending) == true
        pendingPermissionDevice = null
        setBusy(false)
        if (granted) {
            permission.text = getString(R.string.usb_permission_granted)
            appendLog(getString(R.string.usb_log_permission_granted))
            beginProbe(pending)
        } else {
            permission.text = getString(R.string.usb_permission_denied)
            showVerdict(getString(R.string.usb_verdict_permission_denied), R.color.usb_error)
            appendLog(getString(R.string.usb_log_permission_denied))
        }
    }

    private fun handleDetach(intent: Intent) {
        val device = intent.getParcelableExtra<UsbDevice>(UsbManager.EXTRA_DEVICE) ?: return
        if (displayTarget?.device?.deviceName != device.deviceName) {
            refreshDevices()
            return
        }
        stopRequested = true
        permission.text = getString(R.string.usb_device_detached)
        appendLog(getString(R.string.usb_log_device_detached))
        if (running) showVerdict(getString(R.string.usb_verdict_stopping), R.color.usb_warning)
        executor.execute {
            if (activeTarget?.device?.deviceName == device.deviceName && !toneRunning) {
                runCatching { connection?.close() }
                connection = null
                activeTarget = null
                post { clearTargetUi(false) }
            }
        }
        refreshDevices()
    }

    private fun probeSelected() {
        val device = selectedDevice() ?: return
        val manager = usbManager ?: return
        if (manager.hasPermission(device)) {
            permission.text = getString(R.string.usb_permission_granted)
            beginProbe(device)
            return
        }
        pendingPermissionDevice = device
        setBusy(true)
        permission.text = getString(R.string.usb_permission_requesting)
        val intent = Intent(USB_PERMISSION_ACTION).setPackage(packageName)
        val pendingIntent = PendingIntent.getBroadcast(
            this, 1, intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
        manager.requestPermission(device, pendingIntent)
        appendLog(getString(R.string.usb_log_permission_request))
    }

    private fun beginProbe(device: UsbDevice) {
        clearTargetUi(true)
        clearLog()
        stopRequested = false
        setBusy(true)
        showVerdict(getString(R.string.usb_verdict_probing), R.color.usb_running)
        executor.execute { probeDevice(device) }
    }

    private fun probeDevice(device: UsbDevice) {
        var opened: UsbDeviceConnection? = null
        try {
            checkStop()
            runCatching { connection?.close() }
            connection = null
            activeTarget = null
            val manager = usbManager ?: throw IOException(getString(R.string.usb_no_usb_host))
            val conn = manager.openDevice(device) ?: throw IOException(getString(R.string.usb_open_failed))
            opened = conn
            log(getString(R.string.usb_log_opened))
            val (deviceHex, configs) = readDescriptors(conn)
            var lastError = getString(R.string.usb_probe_unknown_error)
            for ((index, configHex) in configs.withIndex()) {
                checkStop()
                val result = ZatCore.usbProbe(deviceHex, configHex, device.productName ?: "")
                val error = result?.optString("error").orEmpty()
                if (result == null || error.isNotEmpty()) {
                    lastError = error.ifEmpty { getString(R.string.usb_probe_native_unavailable) }
                    log(getString(R.string.usb_log_config_rejected, index + 1, lastError))
                    continue
                }
                val rawStreams = jsonObjects(result.optJSONArray("streams"))
                val options = rawStreams.map { parseStream(it) }.toMutableList()
                for (streamIndex in options.indices) {
                    val request = rawStreams[streamIndex].optJSONObject("rateRequest") ?: continue
                    if (options[streamIndex].rates.isNotEmpty()) continue
                    try {
                        val rangeHex = readRateRange(conn, request)
                        val decoded = requireUsb(ZatCore.usbRates(rangeHex), getString(R.string.usb_rates_decode))
                        options[streamIndex] = options[streamIndex].copy(
                            rangeHex = rangeHex,
                            rates = jsonInts(decoded.optJSONArray("rates"))
                        )
                        log(getString(R.string.usb_log_rates, options[streamIndex].interfaceNumber,
                            formatRates(options[streamIndex].rates)))
                    } catch (t: Throwable) {
                        options[streamIndex] = options[streamIndex].copy(rateError = t.message ?: t.javaClass.simpleName)
                        log(getString(R.string.usb_log_rates_failed, options[streamIndex].interfaceNumber,
                            options[streamIndex].rateError))
                    }
                }
                val found = Target(device, deviceHex, configHex, result, options)
                activeTarget = found
                connection = conn
                opened = null
                post {
                    showTarget(found)
                    setBusy(false)
                    showVerdict(getString(R.string.usb_verdict_ready), R.color.usb_idle)
                }
                return
            }
            throw IOException(getString(R.string.usb_probe_failed, lastError))
        } catch (t: Throwable) {
            connection = null
            activeTarget = null
            val message = if (t is TestCancelled) getString(R.string.usb_cancelled) else t.message ?: t.javaClass.simpleName
            post {
                clearTargetUi(true)
                permission.text = getString(R.string.usb_probe_error, message)
                showVerdict(getString(R.string.usb_verdict_probe_failed, message), R.color.usb_error)
                appendLog(getString(R.string.usb_log_probe_failed, message))
                setBusy(false)
            }
        } finally {
            runCatching { opened?.close() }
        }
    }

    private fun showTarget(target: Target) {
        displayTarget = target
        capabilities.text = buildCapabilities(target)
        permission.text = getString(R.string.usb_permission_granted)
        bindSpinner(streams, target.streams.mapIndexed { index, stream ->
            getString(R.string.usb_stream_label, index + 1, stream.interfaceNumber,
                formatEndpoint(stream.endpoint.optInt("address")), syncLabel(stream.endpoint.optString("sync")))
        })
        refreshRateOptions()
        probe.isEnabled = !busy
    }

    private fun clearTargetUi(resetResult: Boolean) {
        displayTarget = null
        rateValues = emptyList()
        bitValues = emptyList()
        channelValues = emptyList()
        capabilities.setText(R.string.usb_capabilities_empty)
        bindSpinner(streams, emptyList())
        bindSpinner(rates, emptyList())
        bindSpinner(bits, emptyList())
        bindSpinner(channels, emptyList())
        asyncWarning.visibility = View.GONE
        if (resetResult) {
            stats.setText(R.string.usb_stats_empty)
            transport.text = getString(R.string.usb_output_aaudio)
            showVerdict(getString(R.string.usb_verdict_idle), R.color.usb_idle)
        }
        run.isEnabled = false
    }

    private fun refreshRateOptions() {
        val stream = currentStream()
        val previous = rateValues.getOrNull(rates.selectedItemPosition)
        val values = stream?.rates.orEmpty().distinct().sorted()
        rateValues = values
        val preferred = previous?.takeIf { it in values }
            ?: values.firstOrNull { it == 44100 }
            ?: values.firstOrNull { it == 48000 }
            ?: values.firstOrNull()
        bindSpinner(rates, values.map { getString(R.string.usb_rate_value, formatRate(it)) }, preferred?.toString())
        refreshBitOptions()
    }

    private fun refreshBitOptions() {
        val stream = currentStream()
        val rate = rateValues.getOrNull(rates.selectedItemPosition) ?: 0
        val previous = bitValues.getOrNull(bits.selectedItemPosition)
        val availableRates = stream?.rates.orEmpty()
        val values = stream?.formats.orEmpty()
            .filter { format -> rate in format.rates || (format.rates.isEmpty() && rate in availableRates) }
            .map { it.bits }.distinct().sorted()
        bitValues = values
        val preferred = previous?.takeIf { it in values } ?: values.firstOrNull { it == 16 } ?: values.firstOrNull()
        bindSpinner(bits, values.map { getString(R.string.usb_bits_value, it) }, preferred?.toString())
        refreshChannelOptions()
    }

    private fun refreshChannelOptions() {
        val stream = currentStream()
        val rate = rateValues.getOrNull(rates.selectedItemPosition) ?: 0
        val selectedBits = bitValues.getOrNull(bits.selectedItemPosition) ?: 0
        val previous = channelValues.getOrNull(channels.selectedItemPosition)
        val availableRates = stream?.rates.orEmpty()
        val values = stream?.formats.orEmpty()
            .filter { format ->
                selectedBits == format.bits &&
                    (rate in format.rates || (format.rates.isEmpty() && rate in availableRates))
            }
            .map { it.channels }.distinct().sorted()
        channelValues = values
        val preferred = previous?.takeIf { it in values } ?: values.firstOrNull { it == 2 } ?: values.firstOrNull()
        bindSpinner(channels, values.map { getString(R.string.usb_channels_value, it) }, preferred?.toString())
        val async = stream?.endpoint?.optString("sync") == "async"
        asyncWarning.visibility = if (async) View.VISIBLE else View.GONE
        run.isEnabled = !busy && !running && currentFormat() != null
    }

    private fun currentStream(): StreamOption? =
        displayTarget?.streams?.getOrNull(streams.selectedItemPosition)

    private fun currentFormat(): FormatOption? {
        val stream = currentStream() ?: return null
        val rate = rateValues.getOrNull(rates.selectedItemPosition) ?: return null
        val selectedBits = bitValues.getOrNull(bits.selectedItemPosition) ?: return null
        val selectedChannels = channelValues.getOrNull(channels.selectedItemPosition) ?: return null
        return stream.formats.firstOrNull {
            it.bits == selectedBits && it.channels == selectedChannels &&
                (rate in it.rates || (it.rates.isEmpty() && rate in stream.rates))
        }
    }

    private fun startSelectedTest() {
        if (busy || running) return
        val target = displayTarget ?: return
        val stream = currentStream() ?: return
        val format = currentFormat() ?: return
        val rate = rateValues.getOrNull(rates.selectedItemPosition) ?: return
        val selectedChannels = channelValues.getOrNull(channels.selectedItemPosition) ?: return
        val durationMS = durationValues.getOrNull(duration.selectedItemPosition) ?: return
        clearLog()
        stopRequested = false
        setBusy(true)
        showVerdict(getString(R.string.usb_verdict_running), R.color.usb_running)
        stats.setText(R.string.usb_stats_empty)
        transport.text = getString(R.string.usb_output_preparing)
        executor.execute {
            runTest(TestRequest(target, stream, format.alt, rate, format.bits, selectedChannels, durationMS))
        }
    }

    private fun runTest(request: TestRequest) {
        var result: JSONObject? = null
        var failure: Throwable? = null
        var cancelled = false
        try {
            checkStop()
            val manager = usbManager ?: throw IOException(getString(R.string.usb_no_usb_host))
            if (!manager.hasPermission(request.target.device)) throw IOException(getString(R.string.usb_permission_denied))
            if (connection == null) {
                connection = manager.openDevice(request.target.device)
                    ?: throw IOException(getString(R.string.usb_open_failed))
            }
            val conn = connection ?: throw IOException(getString(R.string.usb_open_failed))
            val observation = JSONObject()
                .put("interface", request.stream.interfaceNumber)
                .put("alt", request.alt)
                .put("rate", request.rate)
                .put("bits", request.bits)
                .put("channels", request.channels)
            if (request.stream.rangeHex.isNotEmpty()) observation.put("rangeHex", request.stream.rangeHex)
            val plan = requireUsb(
                ZatCore.usbPlan(request.target.deviceHex, request.target.configHex, observation),
                getString(R.string.usb_plan)
            )
            val ifaceNumber = plan.optInt("interface")
            val alt = plan.optInt("alt")
            val endpoint = plan.optJSONObject("endpoint")
                ?: throw IOException(getString(R.string.usb_plan_missing_endpoint))
            val endpointAddress = endpoint.optInt("address")
            if (endpointAddress and 0x80 != 0 || endpoint.optInt("attributes") and 0x03 != 1) {
                throw IOException(getString(R.string.usb_endpoint_invalid))
            }
            log(getString(R.string.usb_log_plan, ifaceNumber, alt, formatRate(plan.optInt("rate")),
                plan.optInt("bits"), plan.optInt("channels")))
            log(getString(R.string.usb_log_endpoint, formatEndpoint(endpointAddress),
                plan.optInt("framesPerSecond"), plan.optInt("maxPacketBytes")))
            plan.optJSONArray("notes")?.let { notes ->
                for (i in 0 until notes.length()) log(notes.optString(i))
            }

            if (request.target.probe.optString("spec") == "uac2") {
                var reply: ByteArray? = null
                try {
                    val validation = requireUsb(
                        ZatCore.usbValidate(ifaceNumber, alt, ""), getString(R.string.usb_validate)
                    )
                    reply = executeRequest(conn, validation)
                } catch (t: Throwable) {
                    log(getString(R.string.usb_log_validate_skipped, t.message ?: t.javaClass.simpleName))
                }
                if (reply != null) {
                    val supported = requireUsb(
                        ZatCore.usbValidate(ifaceNumber, alt, hex(reply)), getString(R.string.usb_validate)
                    )
                    if (!supported.optBoolean("supported")) {
                        throw IOException(getString(R.string.usb_alt_rejected, alt))
                    }
                    log(getString(R.string.usb_log_alt_supported, alt))
                }
            }

            val playback = ZatCore.state()?.optJSONObject("playback")
            if (playback?.optBoolean("playing") == true) {
                if (!ZatCore.pause()) throw IOException(getString(R.string.usb_pause_failed))
                resumeAfterTest = true
                log(getString(R.string.usb_log_paused))
            }
            checkStop()
            val iface = request.target.device.getInterface(ifaceNumber)
                ?: throw IOException(getString(R.string.usb_interface_missing, ifaceNumber))
            if (!conn.claimInterface(iface, true)) {
                throw IOException(getString(R.string.usb_claim_failed, ifaceNumber))
            }
            claimedInterface = iface
            log(getString(R.string.usb_log_claimed, ifaceNumber))
            post { transport.text = getString(R.string.usb_output_claiming) }

            val requests = plan.optJSONArray("requests") ?: JSONArray()
            for (i in 0 until requests.length()) {
                checkStop()
                val control = requests.optJSONObject(i) ?: continue
                executeRequest(conn, control)
                log(getString(R.string.usb_log_control, control.optString("label")))
            }
            checkStop()
            executeRequest(conn, JSONObject()
                .put("bmRequestType", 0x01)
                .put("bRequest", 0x0B)
                .put("wValue", alt)
                .put("wIndex", ifaceNumber)
                .put("label", "SET_INTERFACE"))
            log(getString(R.string.usb_log_alt_selected, ifaceNumber, alt))

            val started = requireUsb(
                ZatCore.usbToneStart(
                    fd = conn.fileDescriptor,
                    endpoint = endpointAddress,
                    rate = plan.optInt("rate"),
                    channels = plan.optInt("channels"),
                    subslot = plan.optInt("subslot"),
                    durationMs = request.durationMS,
                    tones = List(plan.optInt("channels")) { 440.0 },
                    amplitude = 0.25
                ),
                getString(R.string.usb_tone_start)
            )
            if (!started.optBoolean("ok")) {
                throw IOException(started.optString("error", getString(R.string.usb_tone_start_failed)))
            }
            toneRunning = true
            log(getString(R.string.usb_log_bus_clock, speedLabel(started.optString("speed")),
                started.optInt("frames_per_second"), started.optInt("peak_packet_bytes")))
            post {
                setRunning(true)
                transport.text = getString(R.string.usb_output_usb)
                renderStats(started)
            }
            val deadline = System.nanoTime() + (request.durationMS + 5000L) * 1_000_000L
            while (true) {
                checkStop()
                val stat = requireUsb(ZatCore.usbToneStat(), getString(R.string.usb_tone_stat))
                result = stat
                post { renderStats(stat) }
                if (!stat.optBoolean("running")) break
                if (System.nanoTime() > deadline) throw IOException(getString(R.string.usb_test_timeout))
                Thread.sleep(120)
            }
            val stopped = ZatCore.usbToneStop()
            toneRunning = false
            if (stopped != null) result = stopped
        } catch (t: Throwable) {
            failure = t
            cancelled = t is TestCancelled
        } finally {
            if (toneRunning) {
                val stopped = runCatching { ZatCore.usbToneStop() }.getOrNull()
                toneRunning = false
                if (stopped != null) result = stopped
            }
            val hadClaim = claimedInterface != null
            claimedInterface?.let { iface -> runCatching { connection?.releaseInterface(iface) } }
            claimedInterface = null
            runCatching { connection?.close() }
            connection = null
            var resumeWarning: String? = null
            if (resumeAfterTest) {
                val resumed = runCatching { ZatCore.pause() }.getOrDefault(false)
                if (!resumed) resumeWarning = getString(R.string.usb_resume_failed)
                resumeAfterTest = false
            }
            post { renderRunResult(request, result, failure, cancelled, hadClaim, resumeWarning) }
        }
    }

    private fun renderRunResult(
        request: TestRequest,
        result: JSONObject?,
        failure: Throwable?,
        cancelled: Boolean,
        hadClaim: Boolean,
        resumeWarning: String?
    ) {
        setRunning(false)
        if (result != null) renderStats(result)
        val failures = intStat(result, "failures", "failures")
        val shortPackets = intStat(result, "short_packets", "shortPackets")
        val delivered = intStat(result, "delivered", "bytes")
        val completed = intStat(result, "completed", "completed")
        val audioMS = longStat(result, "audio_ms", "audioMs")
        val nativeError = result?.optString("error")?.takeIf { it.isNotEmpty() }
            ?: result?.optJSONObject("report")?.optString("error")?.takeIf { it.isNotEmpty() }
        val problem = when {
            cancelled -> null
            failure != null -> failure.message ?: failure.javaClass.simpleName
            result == null -> getString(R.string.usb_no_report)
            nativeError != null -> nativeError
            failures > 0 -> getString(R.string.usb_fail_failures, failures)
            shortPackets > 0 -> getString(R.string.usb_fail_short, shortPackets)
            delivered <= 0 || completed <= 0 -> getString(R.string.usb_fail_no_audio)
            audioMS < request.durationMS - 150L -> getString(R.string.usb_fail_short_audio, audioMS, request.durationMS)
            else -> null
        }
        val warning = resumeWarning?.let { "\n$it" }.orEmpty()
        when {
            cancelled -> {
                showVerdict(getString(R.string.usb_verdict_cancelled), R.color.usb_warning)
                permission.text = getString(R.string.usb_released)
            }
            problem != null -> {
                showVerdict(getString(R.string.usb_verdict_failed, problem), R.color.usb_error)
                permission.text = getString(R.string.usb_probe_error, problem)
                appendLog(getString(R.string.usb_log_test_failed, problem))
            }
            request.stream.endpoint.optString("sync") == "async" -> {
                showVerdict(getString(R.string.usb_verdict_async_pass), R.color.usb_warning)
                permission.text = getString(R.string.usb_released)
                appendLog(getString(R.string.usb_log_async_pass))
            }
            else -> {
                showVerdict(getString(R.string.usb_verdict_pass), R.color.usb_success)
                permission.text = getString(R.string.usb_released)
                appendLog(getString(R.string.usb_log_pass))
            }
        }
        if (warning.isNotEmpty()) {
            appendLog(resumeWarning.orEmpty())
            transport.text = getString(R.string.usb_output_resume_failed)
        } else {
            transport.text = getString(if (hadClaim) R.string.usb_output_restored else R.string.usb_output_untouched)
        }
        setBusy(false)
    }

    private fun renderStats(stat: JSONObject) {
        val report = stat.optJSONObject("report")
        val runningLabel = getString(if (stat.optBoolean("running")) R.string.usb_running else R.string.usb_finished)
        val error = report?.optString("error").orEmpty()
        stats.text = getString(
            R.string.usb_stats_template,
            runningLabel,
            longStat(stat, "audio_ms", "audioMs") / 1000.0,
            longStat(stat, "elapsed_ms", "elapsedMs") / 1000.0,
            formatBytes(intStat(stat, "delivered", "bytes")),
            intStat(stat, "samples", "samples"),
            intStat(stat, "submitted", "submitted"),
            intStat(stat, "completed", "completed"),
            intStat(stat, "failures", "failures"),
            intStat(stat, "short_packets", "shortPackets"),
            doubleStat(stat, "drift_ppm", "driftPpm")
        ) + if (error.isEmpty()) "" else "\n${getString(R.string.usb_stats_error, error)}"
    }

    private fun requestStop() {
        if (!running && !busy) return
        stopRequested = true
        stop.isEnabled = false
        permission.text = getString(R.string.usb_stopping)
        showVerdict(getString(R.string.usb_verdict_stopping), R.color.usb_warning)
        log(getString(R.string.usb_log_stop_requested))
    }

    private fun setRunning(value: Boolean) {
        running = value
        stop.isEnabled = value
        run.isEnabled = !value && !busy && currentFormat() != null
    }

    private fun setBusy(value: Boolean) {
        busy = value
        progress.visibility = if (value) View.VISIBLE else View.GONE
        refresh.isEnabled = !value
        devices.isEnabled = !value
        probe.isEnabled = !value && selectedDevice() != null
        streams.isEnabled = !value
        rates.isEnabled = !value
        bits.isEnabled = !value
        channels.isEnabled = !value
        duration.isEnabled = !value
        run.isEnabled = !value && !running && currentFormat() != null
        stop.isEnabled = value || running
    }

    private fun readRateRange(conn: UsbDeviceConnection, request: JSONObject): String {
        val header = controlTransfer(conn, request, 2)
        if (header.size < 2) throw IOException(getString(R.string.usb_range_short_header))
        val count = le16(header, 0)
        val limit = request.optInt("expectLen", 512).coerceIn(2, 4096)
        if (count <= 0 || 2 + count * 12 > limit) {
            throw IOException(getString(R.string.usb_range_invalid, count, limit))
        }
        val payload = controlTransfer(conn, request, 2 + count * 12)
        if (payload.size != 2 + count * 12 || le16(payload, 0) != count) {
            throw IOException(getString(R.string.usb_range_short_payload))
        }
        return hex(payload)
    }

    private fun executeRequest(conn: UsbDeviceConnection, request: JSONObject): ByteArray {
        val input = request.optInt("bmRequestType") and 0x80 != 0
        val length = if (input) request.optInt("expectLen") else decodeHex(request.optString("data")).size
        val reply = controlTransfer(conn, request, length)
        if (input && length > 0 && reply.size != length) {
            throw IOException(getString(R.string.usb_control_short, request.optString("label"), reply.size, length))
        }
        return reply
    }

    private fun controlTransfer(conn: UsbDeviceConnection, request: JSONObject, length: Int): ByteArray {
        val input = request.optInt("bmRequestType") and 0x80 != 0
        val data = if (input) null else decodeHex(request.optString("data"))
        val buffer = if (input) ByteArray(length) else data
        val transferred = conn.controlTransfer(
            request.optInt("bmRequestType"),
            request.optInt("bRequest"),
            request.optInt("wValue"),
            request.optInt("wIndex"),
            buffer,
            buffer?.size ?: 0,
            1000
        )
        if (transferred < 0) {
            throw IOException(getString(R.string.usb_control_failed, request.optString("label"), transferred))
        }
        if (!input && transferred != (data?.size ?: 0)) {
            throw IOException(getString(R.string.usb_control_short, request.optString("label"), transferred, data?.size ?: 0))
        }
        return if (input) buffer?.copyOf(transferred) ?: ByteArray(0) else ByteArray(0)
    }

    private fun buildCapabilities(target: Target): String {
        val probe = target.probe
        val out = StringBuilder()
        out.append(getString(R.string.usb_cap_summary,
            probe.optString("spec").uppercase(Locale.US),
            speedLabel(probe.optString("speed")),
            getString(if (probe.optBoolean("microframe")) R.string.usb_microframe else R.string.usb_frame),
            target.streams.size,
            probe.optJSONArray("clocks")?.length() ?: 0
        ))
        target.streams.forEachIndexed { index, stream ->
            val endpoint = stream.endpoint
            out.append("\n\n").append(getString(R.string.usb_cap_stream, index + 1,
                stream.interfaceNumber, formatEndpoint(endpoint.optInt("address")),
                syncLabel(endpoint.optString("sync")), endpoint.optInt("maxPacket"),
                endpoint.optInt("mult"), endpoint.optInt("interval")))
            if (endpoint.optInt("feedbackAddress") != 0) {
                out.append("\n").append(getString(R.string.usb_cap_feedback,
                    formatEndpoint(endpoint.optInt("feedbackAddress"))))
            }
            stream.formats.forEach { format ->
                out.append("\n").append(getString(R.string.usb_cap_format, format.alt, format.bits,
                    format.subslot, format.channels, formatRates(format.rates)))
            }
            if (stream.rates.isNotEmpty() && stream.formats.all { it.rates.isEmpty() }) {
                out.append("\n").append(getString(R.string.usb_cap_rates, formatRates(stream.rates)))
            }
            stream.rateError?.let { out.append("\n").append(getString(R.string.usb_cap_rate_error, it)) }
        }
        val clocks = jsonObjects(probe.optJSONArray("clocks"))
        clocks.forEach { clock ->
            out.append("\n").append(getString(R.string.usb_cap_clock, clock.optInt("id"),
                clockLabel(clock.optString("type")),
                getString(if (clock.optBoolean("syncedToSof")) R.string.usb_sof_synced else R.string.usb_sof_free),
                getString(if (clock.optBoolean("freqControl")) R.string.usb_clock_writable else R.string.usb_clock_fixed)))
        }
        return out.toString()
    }

    private fun parseStream(stream: JSONObject): StreamOption {
        val formats = jsonObjects(stream.optJSONArray("formats")).map {
            FormatOption(it.optInt("alt"), it.optInt("bits"), it.optInt("subslot"),
                it.optInt("channels"), jsonInts(it.optJSONArray("rates")))
        }
        return StreamOption(
            stream.optInt("interface"),
            stream.optJSONObject("endpoint") ?: JSONObject(),
            formats,
            formats.flatMap { it.rates }.distinct().sorted()
        )
    }

    private fun readDescriptors(conn: UsbDeviceConnection): Pair<String, List<String>> {
        val rawDevice = controlTransfer(conn, JSONObject()
            .put("bmRequestType", 0x80)
            .put("bRequest", 0x06)
            .put("wValue", 0x0100)
            .put("wIndex", 0)
            .put("expectLen", 18)
            .put("label", "GET_DESCRIPTOR device"), 18)
        if (rawDevice.size < 18 || rawDevice[1].toInt() != 1) {
            throw IOException(getString(R.string.usb_descriptor_invalid))
        }
        val deviceLength = rawDevice[0].toInt() and 0xff
        if (deviceLength < 18 || deviceLength > rawDevice.size) {
            throw IOException(getString(R.string.usb_descriptor_invalid))
        }
        val device = rawDevice.copyOf(deviceLength)
        val count = device[17].toInt() and 0xff
        if (count == 0) throw IOException(getString(R.string.usb_descriptor_no_config))
        val configs = ArrayList<String>(count)
        repeat(count) { index ->
            val header = controlTransfer(conn, JSONObject()
                .put("bmRequestType", 0x80)
                .put("bRequest", 0x06)
                .put("wValue", 0x0200)
                .put("wIndex", index)
                .put("expectLen", 9)
                .put("label", "GET_DESCRIPTOR configuration header"), 9)
            if (header.size < 9 || header[1].toInt() != 2) {
                throw IOException(getString(R.string.usb_descriptor_invalid))
            }
            val length = le16(header, 2)
            if (length < 9) throw IOException(getString(R.string.usb_descriptor_invalid))
            val config = controlTransfer(conn, JSONObject()
                .put("bmRequestType", 0x80)
                .put("bRequest", 0x06)
                .put("wValue", 0x0200)
                .put("wIndex", index)
                .put("expectLen", length)
                .put("label", "GET_DESCRIPTOR configuration"), length)
            if (config.size != length || config[1].toInt() != 2) {
                throw IOException(getString(R.string.usb_descriptor_invalid))
            }
            configs += hex(config)
        }
        return hex(device) to configs
    }

    private fun bindSpinner(spinner: Spinner, labels: List<String>, preferred: String? = null) {
        val wasUpdating = updatingSpinners
        updatingSpinners = true
        try {
            spinner.adapter = ArrayAdapter(this, android.R.layout.simple_spinner_item, labels).apply {
                setDropDownViewResource(android.R.layout.simple_spinner_dropdown_item)
            }
            val index = preferred?.let { labels.indexOf(it) } ?: -1
            if (index >= 0) spinner.setSelection(index)
        } finally {
            updatingSpinners = wasUpdating
        }
    }

    private fun showVerdict(text: String, colorRes: Int) {
        verdict.text = text
        verdict.backgroundTintList = ColorStateList.valueOf(ContextCompat.getColor(this, colorRes))
    }

    private fun clearLog() {
        logLines.clear()
        logView.text = ""
    }

    private fun log(message: String) = post { appendLog(message) }

    private fun appendLog(message: String) {
        logLines += message
        if (logLines.size > 160) logLines.removeAt(0)
        logView.text = logLines.joinToString("\n")
        logScroll.post { logScroll.fullScroll(View.FOCUS_DOWN) }
    }

    private fun post(action: () -> Unit) {
        if (destroyed) return
        handler.post { if (!destroyed) action() }
    }

    private fun checkStop() {
        if (destroyed || stopRequested) throw TestCancelled()
    }

    private fun requireUsb(value: JSONObject?, operation: String): JSONObject {
        if (value == null) throw IOException("$operation：${getString(R.string.usb_probe_native_unavailable)}")
        val error = value.optString("error")
        if (error.isNotEmpty()) throw IOException("$operation：$error")
        return value
    }

    private fun jsonObjects(array: JSONArray?): List<JSONObject> {
        val out = ArrayList<JSONObject>()
        if (array != null) {
            for (i in 0 until array.length()) array.optJSONObject(i)?.let { out += it }
        }
        return out
    }

    private fun jsonInts(array: JSONArray?): List<Int> {
        val out = ArrayList<Int>()
        if (array != null) {
            for (i in 0 until array.length()) out += array.optInt(i)
        }
        return out
    }

    private fun intStat(stat: JSONObject?, key: String, reportKey: String): Int {
        if (stat == null) return 0
        if (stat.has(key)) return stat.optInt(key)
        return stat.optJSONObject("report")?.optInt(reportKey) ?: 0
    }

    private fun longStat(stat: JSONObject?, key: String, reportKey: String): Long {
        if (stat == null) return 0
        if (stat.has(key)) return stat.optLong(key)
        return stat.optJSONObject("report")?.optLong(reportKey) ?: 0
    }

    private fun doubleStat(stat: JSONObject?, key: String, reportKey: String): Double {
        if (stat == null) return 0.0
        if (stat.has(key)) return stat.optDouble(key)
        return stat.optJSONObject("report")?.optDouble(reportKey) ?: 0.0
    }

    private fun formatRate(rate: Int): String = if (rate % 1000 == 0) {
        "${rate / 1000} kHz"
    } else {
        String.format(Locale.US, "%.1f kHz", rate / 1000.0)
    }

    private fun formatRates(rates: List<Int>): String =
        if (rates.isEmpty()) getString(R.string.usb_rates_unavailable) else rates.joinToString(", ") { formatRate(it) }

    private fun formatBytes(bytes: Int): String = when {
        bytes >= 1024 * 1024 -> String.format(Locale.US, "%.2f MiB", bytes / (1024.0 * 1024.0))
        bytes >= 1024 -> String.format(Locale.US, "%.1f KiB", bytes / 1024.0)
        else -> "$bytes B"
    }

    private fun formatEndpoint(address: Int): String = String.format(Locale.US, "0x%02X", address)

    private fun syncLabel(sync: String): String = getString(when (sync) {
        "async" -> R.string.usb_sync_async
        "adaptive" -> R.string.usb_sync_adaptive
        "vendor" -> R.string.usb_sync_vendor
        else -> R.string.usb_sync_none
    })

    private fun speedLabel(speed: String): String = getString(when (speed) {
        "full" -> R.string.usb_speed_full
        "super" -> R.string.usb_speed_super
        else -> R.string.usb_speed_high
    })

    private fun clockLabel(type: String): String = getString(when (type) {
        "external" -> R.string.usb_clock_external
        "internal-fixed" -> R.string.usb_clock_internal_fixed
        "internal-variable" -> R.string.usb_clock_internal_variable
        else -> R.string.usb_clock_internal_programmable
    })

    private fun hex(bytes: ByteArray): String = bytes.joinToString("") { "%02x".format(it.toInt() and 0xff) }

    private fun decodeHex(value: String): ByteArray {
        require(value.length % 2 == 0) { getString(R.string.usb_hex_invalid) }
        return value.chunked(2).map { it.toInt(16).toByte() }.toByteArray()
    }

    private fun le16(bytes: ByteArray, offset: Int): Int =
        (bytes[offset].toInt() and 0xff) or ((bytes[offset + 1].toInt() and 0xff) shl 8)
}
