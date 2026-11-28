import { theme, tokens } from "./theme";

export function Page() {
  return (
    <main style={{ color: "var(--accent)", gap: theme.space.small, borderWidth: theme.space.line, background: theme.space.surfaceTone }}>
      {tokens.ink}
    </main>
  );
}
