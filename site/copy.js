const copyButton = document.querySelector(".copy");

if (navigator.clipboard?.writeText) {
  copyButton.hidden = false;
  const copyStatus = document.querySelector("#copy-status");
  const commands = [...document.querySelectorAll(".commands code")]
    .map((command) => command.textContent)
    .join("\n");
  let resetLabel;

  copyButton.addEventListener("click", async () => {
    window.clearTimeout(resetLabel);
    try {
      await navigator.clipboard.writeText(commands);
      copyButton.textContent = "Copied";
      copyStatus.textContent = "Quick start commands copied.";
    } catch {
      copyButton.textContent = "Copy failed";
      copyStatus.textContent = "Copy failed. Select the commands to copy them.";
    }
    resetLabel = window.setTimeout(() => {
      copyButton.textContent = "Copy commands";
      copyStatus.textContent = "";
    }, 2500);
  });
}
