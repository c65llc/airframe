import { theme, tokens } from "../theme";

export const Button = () => (
  <button
    style={{
      background: "var(--button-bg)",
      padding: theme.space.small,
      gap: theme.space.space4,
      margin: theme.space.space2,
      border: theme.space.huge,
      outline: tokens.nope,
    }}
  />
);
