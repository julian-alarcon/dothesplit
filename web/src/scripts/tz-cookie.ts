// Set the dts_tz cookie to the device-detected IANA zone on every page
// load. SSR reads this cookie next request and renders timestamps in the
// user's local time without a flash. Loaded as an external module (not
// <script is:inline>) so Vite emits it as a hashed asset that the strict
// CSP `script-src 'self'` allows; raw inline scripts in .astro sources
// aren't auto-hashed by Astro and would silently get blocked.
(() => {
  try {
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
    if (!tz) return;
    if (!document.cookie.split("; ").some((c) => c === "dts_tz=" + tz)) {
      document.cookie =
        "dts_tz=" +
        encodeURIComponent(tz) +
        "; Path=/; Max-Age=31536000; SameSite=Lax";
    }
  } catch (_) {
    // Older browsers without Intl resolvedOptions - leave the cookie as-is.
  }
})();
