import { defineConfig } from "astro/config";
import node from "@astrojs/node";
import icon from "astro-icon";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  output: "server",
  adapter: node({ mode: "standalone" }),
  integrations: [icon()],
  server: {
    host: "0.0.0.0",
    port: 3000,
  },
  security: {
    checkOrigin: false,
    // CSP: Astro auto-hashes <script is:inline> blocks; Tailwind-injected
    // inline <style> tags still need 'unsafe-inline'.
    csp: {
      algorithm: "SHA-256",
      styleDirective: {
        resources: ["'self'", "'unsafe-inline'"],
      },
    },
  },
  vite: {
    plugins: [tailwindcss()],
  },
});
