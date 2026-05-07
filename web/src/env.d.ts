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
    } | null;
    /** Raw cookie header forwarded to the API on server-side fetches. */
    cookie: string;
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
