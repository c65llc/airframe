import SwiftUI

struct PrimaryButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .background(DesignTokens.buttonBg)
            .foregroundStyle(DesignTokens.`buttonInk`)
    }
}
