import SwiftUI

struct ContentView: View {
    var body: some View {
        Text("Hello")
            .foregroundStyle(DesignTokens.ink)
            .background(DesignTokens.surface)
            .environment(\.density, DesignTokens.Density.compact)
            .padding(AppTheme.spacing.small)
            .border(Theme.accent, width: AppTheme.spacing.hairline)
    }
}
