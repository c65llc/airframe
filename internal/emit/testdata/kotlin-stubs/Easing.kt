// Stub of the Compose API the kotlin emitter uses, for TestKotlinTypechecks:
// the real packages and signatures, no behaviour.

package androidx.compose.animation.core

public fun interface Easing {
    public fun transform(fraction: Float): Float
}

public class CubicBezierEasing(
    private val a: Float,
    private val b: Float,
    private val c: Float,
    private val d: Float,
) : Easing {
    override fun transform(fraction: Float): Float = fraction
}
