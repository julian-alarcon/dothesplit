// Reformat <time data-fmt="…"> elements to the viewer's local timezone (or
// to a stored override when <html data-tz-override="…"> is set). SSR emits
// the canonical UTC ISO string in the `datetime` attribute and a server-
// rendered fallback in the text content; this script replaces the text with
// a locale-aware string after first paint. Runs once on DOMContentLoaded -
// pages that swap content (e.g. activity-fragment) call refresh() manually.

type FormatName = "datetime-short" | "date-medium";

const FORMATS: Record<FormatName, Intl.DateTimeFormatOptions> = {
  "datetime-short": { dateStyle: "medium", timeStyle: "short" },
  "date-medium": { dateStyle: "medium" },
};

function timezoneOverride(): string | undefined {
  const tz = document.documentElement.dataset.tzOverride;
  return tz && tz.length > 0 ? tz : undefined;
}

function formatOne(el: HTMLTimeElement): void {
  const iso = el.getAttribute("datetime");
  if (!iso) return;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return;
  const fmt = el.dataset.fmt as FormatName | undefined;
  if (!fmt || !(fmt in FORMATS)) return;
  const options = { ...FORMATS[fmt], timeZone: timezoneOverride() };
  try {
    el.textContent = new Intl.DateTimeFormat(undefined, options).format(d);
  } catch {
    // Bad zone (shouldn't happen with a server-validated override) - leave the
    // server-rendered text in place.
  }
}

export function refresh(root: ParentNode = document): void {
  for (const el of root.querySelectorAll<HTMLTimeElement>("time[data-fmt]")) {
    formatOne(el);
  }
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", () => refresh());
} else {
  refresh();
}
