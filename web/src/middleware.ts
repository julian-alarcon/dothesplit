import { defineMiddleware } from "astro/middleware";
import { apiFor } from "@/lib/api/client";
import { resolveTimezone } from "@/lib/timezone";
import { resolveLocale } from "@/lib/locale";

export const onRequest = defineMiddleware(async (ctx, next) => {
  const cookie = ctx.request.headers.get("cookie") ?? "";
  ctx.locals.cookie = cookie;
  ctx.locals.user = null;
  ctx.locals.setupLocked = true;

  const path = ctx.url.pathname;

  // First-run setup probe. The endpoint is public (no cookie needed) and
  // returns one bool. Default to locked=true if the API is unreachable so a
  // network failure never accidentally exposes the install flow.
  try {
    const status = await apiFor("").GET("/v1/setup/status");
    if (status.data) ctx.locals.setupLocked = status.data.locked;
  } catch {
    ctx.locals.setupLocked = true;
  }

  // While setup is unlocked, every UI route except /setup itself and the
  // SSR forwarder under /api/setup is redirected to /setup. We also skip
  // resolving the user (there are no users to authenticate as).
  if (!ctx.locals.setupLocked) {
    if (
      path !== "/setup" &&
      !path.startsWith("/api/setup") &&
      path !== "/favicon.ico"
    ) {
      return ctx.redirect("/setup");
    }
    ctx.locals.timezone = resolveTimezone(undefined, cookie);
    ctx.locals.locale = resolveLocale(ctx.request.headers.get("accept-language"));
    return next();
  }

  // Setup is locked → resolve the session as usual.
  if (cookie.includes("dts_session=")) {
    const api = apiFor(cookie);
    const { data } = await api.GET("/v1/me");
    if (data) ctx.locals.user = data;
  }

  ctx.locals.timezone = resolveTimezone(ctx.locals.user?.timezone, cookie);
  ctx.locals.locale = resolveLocale(ctx.request.headers.get("accept-language"));

  // Stale bookmark: never let an authenticated visitor see /setup again.
  if (path === "/setup") {
    return ctx.redirect(ctx.locals.user ? "/groups" : "/login");
  }

  const isPublic =
    path === "/login" ||
    path === "/register" ||
    path === "/verify" ||
    path === "/forgot" ||
    path === "/reset" ||
    path === "/credits" ||
    path.startsWith("/api/") ||
    path === "/favicon.ico";

  if (!ctx.locals.user && !isPublic) {
    return ctx.redirect("/login");
  }
  if (ctx.locals.user && (path === "/login" || path === "/register")) {
    return ctx.redirect("/groups");
  }

  // Admin guard: any /admin/* page requires the role flag on the resolved
  // user. Non-admins land on /groups instead of seeing a 403 leak.
  if (path.startsWith("/admin") && !ctx.locals.user?.is_admin) {
    return ctx.redirect("/groups");
  }

  // Admin SSR responses must not be cached by intermediaries.
  if (path.startsWith("/admin")) {
    const res = await next();
    res.headers.set("Cache-Control", "no-store");
    res.headers.set("X-Frame-Options", "DENY");
    return res;
  }
  return next();
});
