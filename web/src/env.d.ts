/// <reference types="astro/client" />

declare namespace App {
  interface Locals {
    user: {
      id: string;
      email: string;
      display_name: string;
      created_at: string;
      has_avatar: boolean;
      avatar_updated_at?: string | null;
      deleted_at?: string | null;
      week_start: 0 | 1;
      timezone?: string | null;
    } | null;
    /** Raw cookie header forwarded to the API on server-side fetches. */
    cookie: string;
    /**
     * Resolved IANA timezone for this request. Source priority:
     *   1. user.timezone (stored override)
     *   2. dts_tz cookie (device-detected on first paint)
     *   3. "UTC" (server fallback)
     */
    timezone: string;
    /**
     * Resolved BCP-47 locale for this request, derived from Accept-Language.
     * Falls back to "en-US". Used for Intl.DisplayNames (currency names),
     * Intl.NumberFormat, etc. Future i18n work will layer a stored
     * per-user override on top.
     */
    locale: string;
  }
}

interface ImportMetaEnv {
  /** Internal URL used by the Astro SSR process to call the Go API. */
  readonly API_BASE_URL_INTERNAL: string;
  /** Browser-facing base URL (for future client-side calls). */
  readonly PUBLIC_API_BASE_URL: string;
}
interface ImportMeta {
  readonly env: ImportMetaEnv;
}
