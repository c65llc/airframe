import SwiftUI

struct ContentView: View {
    var body: some View {
        let tokens = DesignTokens(density: .compact)
        return Text("Hello")
            .foregroundStyle(DesignTokens.ink)
            .padding(DesignTokens.gap)
            .background(DesignTokens.surfce)
            .environment(\.density, DesignTokens.Density.compact)
            .padding(AppTheme.spacing.small)
            .padding(AppTheme.spacing.space4)
            .padding(AppTheme.spacing.large)
            .tint(Theme.accent)
            .opacity(MyDesignTokens.fade)
            // airframe-exempt token-owner-violation: preview of the button palette
            .border(DesignTokens.buttonBg, width: tokens.hairline)
    }
}
