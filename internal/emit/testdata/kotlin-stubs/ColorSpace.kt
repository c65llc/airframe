// Stub of the Compose API the kotlin emitter uses, for TestKotlinTypechecks:
// the real packages and signatures, no behaviour.

package androidx.compose.ui.graphics.colorspace

public abstract class ColorSpace internal constructor(public val name: String)

public class Rgb internal constructor(name: String) : ColorSpace(name)

public object ColorSpaces {
    public val Srgb: Rgb = Rgb("sRGB IEC61966-2.1")
    public val DisplayP3: Rgb = Rgb("Display P3")
}
