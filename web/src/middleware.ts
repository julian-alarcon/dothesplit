import { defineMiddleware } from "astro/middleware";
import { apiFor } from "@/lib/api/client";
import { resolveTimezone } from "@/lib/timezone";
import { resolveLocale } from "@/lib/locale";

export const onRequest = defineMiddleware(async (ctx, next) => {
  const cookie = ctx.request.headers.get("cookie") ?? "";
  ctx.locals.cookie = cookie;
  ctx.locals.user = null;

  if (cookie.includes("dts_session=")) {
    const api = apiFor(cookie);
    const { data } = await api.GET("/v1/me");
    if (data) ctx.locals.user = data;
  }

  ctx.locals.timezone = resolveTimezone(ctx.locals.user?.timezone, cookie);
  ctx.locals.locale = resolveLocale(ctx.request.headers.get("accept-language"));

  const path = ctx.url.pathname;
  const isPublic =
    path === "/login" ||
    path === "/register" ||
    path === "/credits" ||
    path.startsWith("/api/") ||
    path === "/favicon.ico";

  if (!ctx.locals.user && !isPublic) {
    return ctx.redirect("/login");
  }
  if (ctx.locals.user && (path === "/login" || path === "/register")) {
    return ctx.redirect("/groups");
  }
  return next();
});
