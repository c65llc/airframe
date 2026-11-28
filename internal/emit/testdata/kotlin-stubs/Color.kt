// Stub of the Compose API the kotlin emitter uses, for TestKotlinTypechecks:
// the real packages and signatures, no behaviour.

package androidx.compose.ui.graphics

import androidx.compose.ui.graphics.colorspace.ColorSpace
import androidx.compose.ui.graphics.colorspace.ColorSpaces

@JvmInline
public value class Color(public val value: ULong)

public fun Color(color: Long): Color = Color(color.toULong())

public fun Color(color: Int): Color = Color(color.toLong())

public fun Color(
    red: Float,
    green: Float,
    blue: Float,
    alpha: Float = 1f,
    colorSpace: ColorSpace = ColorSpaces.Srgb,
): Color = Color(0L)
