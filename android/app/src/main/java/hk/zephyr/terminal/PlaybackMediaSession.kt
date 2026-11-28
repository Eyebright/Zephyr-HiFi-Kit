package hk.zephyr.terminal

import android.app.Activity
import android.content.Intent
import android.media.MediaMetadata
import android.media.session.MediaSession
import android.media.session.PlaybackState
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.view.KeyEvent
import org.json.JSONObject
import java.util.concurrent.Executors

class PlaybackMediaSession(
    private val activity: Activity,
    private val onError: (String) -> Unit
) {
    private val main = Handler(Looper.getMainLooper())
    private val executor = Executors.newFixedThreadPool(2)
    private val session = MediaSession(activity, "ZephyrHiFiPlayback")
    @Volatile private var started = false
    private var connected = false
    private var stateName = ""

    private val poll = object : Runnable {
        override fun run() {
            if (!started) return
            executor.execute {
                val state = runCatching { ZatCore.state() }.getOrNull()
                main.post {
                    if (started) update(state)
                    if (started) main.postDelayed(this, 1000)
                }
            }
        }
    }

    private val callback = object : MediaSession.Callback() {
        override fun onMediaButtonEvent(mediaButtonIntent: Intent): Boolean =
            handleKeyEvent(keyEvent(mediaButtonIntent))

        override fun onPlay() = play()
        override fun onPause() = pause()
        override fun onSkipToNext() = next()
        override fun onSkipToPrevious() = previous()
        override fun onStop() = stopPlayback()
    }

    init {
        session.setFlags(MediaSession.FLAG_HANDLES_MEDIA_BUTTONS or MediaSession.FLAG_HANDLES_TRANSPORT_CONTROLS)
        session.setCallback(callback, null)
        session.setPlaybackState(PlaybackState.Builder().setState(PlaybackState.STATE_NONE, 0, 0f).build())
    }

    fun start() {
        if (started) return
        started = true
        main.removeCallbacks(poll)
        main.post(poll)
    }

    fun stop() {
        started = false
        main.removeCallbacks(poll)
        session.isActive = false
        session.setPlaybackState(PlaybackState.Builder().setState(PlaybackState.STATE_NONE, 0, 0f).build())
        session.setMetadata(null)
    }

    fun release() {
        stop()
        executor.shutdownNow()
        session.release()
    }

    fun handleKeyEvent(event: KeyEvent?): Boolean {
        if (!started || event == null || !isMediaKey(event.keyCode)) return false
        if (event.action == KeyEvent.ACTION_DOWN && event.repeatCount == 0) {
            when (event.keyCode) {
                KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE -> togglePause()
                KeyEvent.KEYCODE_MEDIA_PLAY -> play()
                KeyEvent.KEYCODE_MEDIA_PAUSE -> pause()
                KeyEvent.KEYCODE_MEDIA_NEXT, KeyEvent.KEYCODE_MEDIA_FAST_FORWARD,
                KeyEvent.KEYCODE_MEDIA_SKIP_FORWARD -> next()
                KeyEvent.KEYCODE_MEDIA_PREVIOUS, KeyEvent.KEYCODE_MEDIA_REWIND,
                KeyEvent.KEYCODE_MEDIA_SKIP_BACKWARD -> previous()
                KeyEvent.KEYCODE_MEDIA_STOP -> stopPlayback()
            }
        }
        return true
    }

    private fun keyEvent(intent: Intent): KeyEvent? =
        if (Build.VERSION.SDK_INT >= 33) {
            intent.getParcelableExtra(Intent.EXTRA_KEY_EVENT, KeyEvent::class.java)
        } else {
            @Suppress("DEPRECATION")
            intent.getParcelableExtra(Intent.EXTRA_KEY_EVENT)
        }

    private fun isMediaKey(keyCode: Int): Boolean = when (keyCode) {
        KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE,
        KeyEvent.KEYCODE_MEDIA_PLAY,
        KeyEvent.KEYCODE_MEDIA_PAUSE,
        KeyEvent.KEYCODE_MEDIA_NEXT,
        KeyEvent.KEYCODE_MEDIA_PREVIOUS,
        KeyEvent.KEYCODE_MEDIA_FAST_FORWARD,
        KeyEvent.KEYCODE_MEDIA_REWIND,
        KeyEvent.KEYCODE_MEDIA_SKIP_FORWARD,
        KeyEvent.KEYCODE_MEDIA_SKIP_BACKWARD,
        KeyEvent.KEYCODE_MEDIA_STOP -> true
        else -> false
    }

    private fun play() {
        if (started && stateName == "paused") togglePause()
    }

    private fun pause() {
        if (started && stateName == "playing") togglePause()
    }

    private fun togglePause() {
        if (!started) return
        submit("播放/暂停失败") { ZatCore.pauseResult() }
    }

    private fun next() {
        if (!started) return
        submit("下一首失败") { ZatCore.queueNext() }
    }

    private fun previous() {
        if (!started) return
        submit("上一首失败") { ZatCore.queuePrev() }
    }

    private fun stopPlayback() {
        if (!started) return
        executor.execute {
            ZatCore.stopPlayback()
            val state = runCatching { ZatCore.state() }.getOrNull()
            main.post { if (started) update(state) }
        }
    }

    private fun submit(message: String, action: () -> JSONObject?) {
        executor.execute {
            try {
                val result = action()
                val error = result?.optString("error")
                if (!error.isNullOrEmpty()) throw IllegalStateException(error)
            } catch (error: Throwable) {
                main.post {
                    if (started) onError("$message：${error.message ?: "未知错误"}")
                }
            } finally {
                val state = runCatching { ZatCore.state() }.getOrNull()
                main.post { if (started) update(state) }
            }
        }
    }

    private fun update(root: JSONObject?) {
        val playback = root?.optJSONObject("playback") ?: return
        connected = playback.optBoolean("connected")
        stateName = playback.optString("state")
        val title = playback.optString("title")
        val hasMedia = connected && (
            playback.optString("track_id").isNotEmpty() ||
                playback.optString("session_id").isNotEmpty() ||
                title.isNotEmpty() ||
                stateName in setOf("playing", "paused", "ready", "buffering")
            )
        val state = when {
            stateName == "playing" -> PlaybackState.STATE_PLAYING
            stateName == "paused" -> PlaybackState.STATE_PAUSED
            stateName in setOf("ready", "buffering") && hasMedia -> PlaybackState.STATE_BUFFERING
            stateName == "error" -> PlaybackState.STATE_ERROR
            hasMedia -> PlaybackState.STATE_PAUSED
            else -> PlaybackState.STATE_NONE
        }
        var actions = 0L
        if (connected) {
            actions = PlaybackState.ACTION_PLAY_PAUSE.toLong() or
                PlaybackState.ACTION_SKIP_TO_NEXT.toLong() or
                PlaybackState.ACTION_SKIP_TO_PREVIOUS.toLong() or
                PlaybackState.ACTION_STOP.toLong()
            if (hasMedia) actions = actions or PlaybackState.ACTION_SEEK_TO.toLong()
        }
        val rate = playback.optInt("rate")
        val pts = playback.optLong("pts")
        val position = if (rate > 0) pts * 1000L / rate else 0L
        val speed = if (state == PlaybackState.STATE_PLAYING) 1f else 0f
        session.isActive = started && connected
        session.setPlaybackState(
            PlaybackState.Builder()
                .setState(state, position, speed)
                .setActions(actions)
                .build()
        )
        if (title.isNotEmpty()) {
            val metadata = MediaMetadata.Builder()
                .putString(MediaMetadata.METADATA_KEY_TITLE, title)
                .putString(MediaMetadata.METADATA_KEY_ARTIST, playback.optString("artist"))
                .putString(MediaMetadata.METADATA_KEY_ALBUM, playback.optString("album"))
                .putLong(MediaMetadata.METADATA_KEY_DURATION, playback.optLong("duration_ms"))
                .build()
            session.setMetadata(metadata)
        } else {
            session.setMetadata(null)
        }
    }
}
