package com.team27.mama.spike

import android.Manifest
import android.annotation.SuppressLint
import android.content.Context
import android.content.pm.PackageManager
import android.hardware.camera2.CameraCaptureSession
import android.hardware.camera2.CameraCharacteristics
import android.hardware.camera2.CameraDevice
import android.hardware.camera2.CameraManager
import android.media.MediaRecorder
import android.os.Build
import android.os.Handler
import android.os.HandlerThread
import android.os.SystemClock
import java.io.File
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException
import kotlinx.coroutines.delay
import kotlinx.coroutines.suspendCancellableCoroutine

/**
 * Spike question: can the locked-phone trigger record video with no camera
 * app and nothing on screen? Records [durationMs] of back-camera video (no
 * audio: the mic probe holds the mic) straight to files/spike-cam-<id>.mp4
 * and returns a one-line result. Nothing is sent: the contract has no video.
 */
object CameraProbe {
    @SuppressLint("MissingPermission")
    suspend fun record(context: Context, incidentId: String, durationMs: Long = 3_000): String {
        if (context.checkSelfPermission(Manifest.permission.CAMERA) != PackageManager.PERMISSION_GRANTED) return "no CAMERA permission"
        val manager = context.getSystemService(CameraManager::class.java)
        val cameraId = try {
            manager.cameraIdList.firstOrNull {
                manager.getCameraCharacteristics(it).get(CameraCharacteristics.LENS_FACING) == CameraCharacteristics.LENS_FACING_BACK
            }
        } catch (e: Exception) {
            return "camera lookup failed ${e.javaClass.simpleName}: ${e.message}"
        } ?: return "no back camera"
        val file = File(context.filesDir, "spike-cam-$incidentId.mp4")
        val thread = HandlerThread("spike-cam").apply { start() }
        val handler = Handler(thread.looper)
        @Suppress("DEPRECATION")
        val recorder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) MediaRecorder(context) else MediaRecorder()
        var device: CameraDevice? = null
        var session: CameraCaptureSession? = null
        val start = SystemClock.elapsedRealtime()
        return try {
            recorder.setVideoSource(MediaRecorder.VideoSource.SURFACE)
            recorder.setOutputFormat(MediaRecorder.OutputFormat.MPEG_4)
            recorder.setOutputFile(file.absolutePath)
            recorder.setVideoEncoder(MediaRecorder.VideoEncoder.H264)
            recorder.setVideoSize(1280, 720)
            recorder.setVideoFrameRate(30)
            recorder.setVideoEncodingBitRate(4_000_000)
            recorder.setOrientationHint(90)
            recorder.prepare()

            val opened = open(manager, cameraId, handler).also { device = it }
            val openMs = SystemClock.elapsedRealtime() - start
            val configured = configure(opened, recorder, handler).also { session = it }
            val request = opened.createCaptureRequest(CameraDevice.TEMPLATE_RECORD).apply { addTarget(recorder.surface) }.build()
            configured.setRepeatingRequest(request, null, handler)
            recorder.start()
            delay(durationMs)
            recorder.stop()
            "recorded ${file.name} bytes=${file.length()} openMs=$openMs"
        } catch (e: Exception) {
            "camera failed ${e.javaClass.simpleName}: ${e.message}"
        } finally {
            runCatching { session?.close() }
            runCatching { device?.close() }
            recorder.release()
            thread.quitSafely()
        }
    }

    @SuppressLint("MissingPermission")
    private suspend fun open(manager: CameraManager, id: String, handler: Handler): CameraDevice =
        suspendCancellableCoroutine { cont ->
            manager.openCamera(id, object : CameraDevice.StateCallback() {
                override fun onOpened(camera: CameraDevice) = cont.resume(camera)
                override fun onDisconnected(camera: CameraDevice) {
                    camera.close()
                    if (cont.isActive) cont.resumeWithException(IllegalStateException("camera disconnected"))
                }
                override fun onError(camera: CameraDevice, error: Int) {
                    camera.close()
                    if (cont.isActive) cont.resumeWithException(IllegalStateException("camera error $error"))
                }
            }, handler)
        }

    @Suppress("DEPRECATION")
    private suspend fun configure(device: CameraDevice, recorder: MediaRecorder, handler: Handler): CameraCaptureSession =
        suspendCancellableCoroutine { cont ->
            device.createCaptureSession(listOf(recorder.surface), object : CameraCaptureSession.StateCallback() {
                override fun onConfigured(session: CameraCaptureSession) = cont.resume(session)
                override fun onConfigureFailed(session: CameraCaptureSession) =
                    cont.resumeWithException(IllegalStateException("capture session configure failed"))
            }, handler)
        }
}
