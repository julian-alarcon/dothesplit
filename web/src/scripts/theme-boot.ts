// Apply the stored theme before paint so we never flash the wrong colors.
// Default = "dark" when nothing is stored. Valid values: "dark" | "light" |
// "high-contrast". Loaded synchronously in <head> via the matching <script src>
// in Base.astro — Vite emits this as a hashed asset that the strict CSP
// `script-src 'self'` allows, unlike a raw <script is:inline> block which
// Astro does not auto-hash.
(() => {
  try {
    const stored = localStorage.getItem("dts_theme");
    const valid =
      stored === "light" || stored === "dark" || stored === "high-contrast";
    document.documentElement.dataset.theme = valid ? stored : "dark";
  } catch (_) {
    document.documentElement.dataset.theme = "dark";
  }
})();
