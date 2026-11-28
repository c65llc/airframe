import { theme, tokens } from "./theme";

export function Page() {
  return (
    <main style={{ "--card-pad": "12px", color: "var(--button-ink)", gap: theme.space.small }}>
      {tokens.accent}
    </main>
  );
}
