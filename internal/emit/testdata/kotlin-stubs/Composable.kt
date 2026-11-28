// Stub of the Compose API the android guide's snippets use, for
// TestGuideKotlinCompiles: the real package and targets, no behaviour.
// Without the Compose compiler plugin the annotation changes nothing, so a
// @Composable function compiles and calls like any other.

package androidx.compose.runtime

@MustBeDocumented
@Retention(AnnotationRetention.BINARY)
@Target(
    AnnotationTarget.FUNCTION,
    AnnotationTarget.TYPE,
    AnnotationTarget.TYPE_PARAMETER,
    AnnotationTarget.PROPERTY_GETTER,
)
public annotation class Composable
