// Stub of the Compose API the kotlin emitter uses, for TestKotlinTypechecks:
// the real packages and signatures, no behaviour.

package androidx.compose.ui.unit

@JvmInline
public value class Dp(public val value: Float)

public inline val Int.dp: Dp get() = Dp(this.toFloat())

public inline val Double.dp: Dp get() = Dp(this.toFloat())

public inline val Float.dp: Dp get() = Dp(this)
