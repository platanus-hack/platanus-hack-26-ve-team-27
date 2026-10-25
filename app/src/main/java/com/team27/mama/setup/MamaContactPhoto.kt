package com.team27.mama.setup

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import androidx.annotation.DrawableRes
import com.team27.mama.R
import java.io.ByteArrayOutputStream
import kotlin.math.max
import kotlin.math.roundToInt

/** The illustration used as the "Mamá" contact photo. Swap the drawable here. */
@DrawableRes
const val MAMA_PHOTO_RESOURCE = R.drawable.mama_lista

/** Longest side of the display photo; the contacts provider downsizes it to its own limit. */
const val DISPLAY_PHOTO_MAX_PX = 720

/** Longest side of the thumbnail stored in the Photo data row. */
const val THUMBNAIL_MAX_PX = 256

private const val JPEG_QUALITY = 90

/** The contact photo in the two sizes the contacts provider keeps, both JPEG. */
class MamaPhoto(val displayJpeg: ByteArray, val thumbnailJpeg: ByteArray)

/** What to do with the contact's Photo data row so it never has two. */
sealed interface PhotoRowPlan {
    /** The raw contact has no photo row yet. */
    data object Insert : PhotoRowPlan

    /** Overwrite the existing row [dataId]. */
    data class Update(val dataId: Long) : PhotoRowPlan
}

/** Picks the plan from the photo rows the raw contact has now: reuse the first, never add a second. */
fun planPhotoRow(existingDataIds: List<Long>): PhotoRowPlan =
    existingDataIds.firstOrNull()?.let { PhotoRowPlan.Update(it) } ?: PhotoRowPlan.Insert

/** Size that fits [width] x [height] inside [maxPx] on its longest side, never upscaling. */
fun fitWithin(width: Int, height: Int, maxPx: Int): Pair<Int, Int> {
    val longest = max(width, height)
    if (longest <= maxPx) return width to height
    val scale = maxPx.toFloat() / longest
    return max(1, (width * scale).roundToInt()) to max(1, (height * scale).roundToInt())
}

/** Decodes [MAMA_PHOTO_RESOURCE] (a webp) and re-encodes it as JPEG. Null when it cannot be decoded. */
fun loadMamaPhoto(context: Context): MamaPhoto? {
    val source = BitmapFactory.decodeResource(context.resources, MAMA_PHOTO_RESOURCE) ?: return null
    return try {
        MamaPhoto(
            displayJpeg = source.toJpeg(DISPLAY_PHOTO_MAX_PX),
            thumbnailJpeg = source.toJpeg(THUMBNAIL_MAX_PX),
        )
    } finally {
        source.recycle()
    }
}

private fun Bitmap.toJpeg(maxPx: Int): ByteArray {
    val (w, h) = fitWithin(width, height, maxPx)
    val scaled = if (w == width && h == height) this else Bitmap.createScaledBitmap(this, w, h, true)
    return try {
        ByteArrayOutputStream().also { scaled.compress(Bitmap.CompressFormat.JPEG, JPEG_QUALITY, it) }.toByteArray()
    } finally {
        if (scaled !== this) scaled.recycle()
    }
}
