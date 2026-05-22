import type { APIRoute } from "astro";

const internalBase =
  process.env.API_BASE_URL_INTERNAL ?? "http://localhost:8080";

export const POST: APIRoute = async ({ request, redirect }) => {
  const form = await request.formData();
  const cookie = request.headers.get("cookie") ?? "";
  const res = await fetch(`${internalBase}/v1/me/email/change-confirm`, {
    method: "POST",
    headers: { "Content-Type": "application/json", cookie },
    body: JSON.stringify({ code: form.get("code") }),
  });
  if (res.status === 400) return redirect("/account?error=email_confirm_invalid&ok=email_requested", 302);
  if (res.status === 410) return redirect("/account?error=email_confirm_expired", 302);
  if (res.status === 409) return redirect("/account?error=email_taken", 302);
  if (!res.ok) return redirect("/account?error=email_confirm_invalid&ok=email_requested", 302);
  // The API rotated the session - forward Set-Cookie back to the browser so
  // the new cookie replaces the old one.
  const headers = new Headers();
  for (const c of res.headers.getSetCookie?.() ?? []) {
    headers.append("set-cookie", c);
  }
  headers.set("location", "/account?ok=email_changed");
  return new Response(null, { status: 302, headers });
};
