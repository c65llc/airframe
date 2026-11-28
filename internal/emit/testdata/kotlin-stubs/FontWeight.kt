// Stub of the Compose API the kotlin emitter uses, for TestKotlinTypechecks:
// the real packages and signatures, no behaviour.

package androidx.compose.ui.text.font

public class FontWeight(public val weight: Int) : Comparable<FontWeight> {
    override fun compareTo(other: FontWeight): Int = weight.compareTo(other.weight)
}
