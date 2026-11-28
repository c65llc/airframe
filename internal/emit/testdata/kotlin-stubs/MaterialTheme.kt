// Stub of the Compose API the android guide's snippets use, for
// TestGuideKotlinCompiles: the real package, and MaterialTheme's
// colorScheme and content parameters (the real one also takes shapes and
// typography, which the guide does not pass), no behaviour.

package androidx.compose.material3

import androidx.compose.runtime.Composable

@Composable
public fun MaterialTheme(
    colorScheme: ColorScheme = lightColorScheme(),
    content: @Composable () -> Unit,
) {
    content()
}
