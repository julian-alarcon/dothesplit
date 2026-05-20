// Footer theme picker. Per-device only: the value lives in localStorage
// (key: dts_theme) and never leaves the browser. The inline boot script in
// Base.astro applies the resolved theme before paint; this script syncs the
// <select> to the stored value and writes localStorage on change.
//
// Default = "dark" when nothing is stored.

type Theme = "dark" | "light" | "high-contrast";

const STORAGE_KEY = "dts_theme";

function isTheme(v: string | null): v is Theme {
  return v === "dark" || v === "light" || v === "high-contrast";
}

const select = document.querySelector<HTMLSelectElement>("[data-theme-picker]");
if (select) {
  let stored: string | null = null;
  try {
    stored = localStorage.getItem(STORAGE_KEY);
  } catch {
    // Storage may be blocked (private mode); picker still works in-session.
  }
  select.value = isTheme(stored) ? stored : "dark";

  select.addEventListener("change", () => {
    const picked = select.value;
    if (!isTheme(picked)) return;
    try {
      localStorage.setItem(STORAGE_KEY, picked);
    } catch {
      // Silently degrade.
    }
    document.documentElement.dataset.theme = picked;
  });
}
