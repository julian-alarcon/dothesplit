import type { APIRoute } from "astro";

const internalBase =
  process.env.API_BASE_URL_INTERNAL ?? "http://localhost:8080";

// POST /api/me-password: handles the change-password form on
// /account/password. Validates that `password_confirmation` matches
// `new_password` server-side too (we can't trust the inline JS check), then
// forwards to the Go API which enforces the current-password gate.
export const POST: APIRoute = async ({ request, redirect }) => {
  const form = await request.formData();
  const cookie = request.headers.get("cookie") ?? "";
  const old_password = (form.get("old_password") ?? "").toString();
  const new_password = (form.get("new_password") ?? "").toString();
  const password_confirmation = (form.get("password_confirmation") ?? "").toString();

  if (!old_password) {
    return redirect("/account/password?error=current", 302);
  }
  if (new_password.length < 10) {
    return redirect("/account/password?error=length", 302);
  }
  if (new_password !== password_confirmation) {
    return redirect("/account/password?error=mismatch", 302);
  }

  const res = await fetch(`${internalBase}/v1/me/password`, {
    method: "POST",
    headers: { "Content-Type": "application/json", cookie },
    body: JSON.stringify({ old_password, new_password }),
  });
  if (res.status === 401) {
    return redirect("/account/password?error=wrong_current", 302);
  }
  if (!res.ok) {
    return redirect("/account/password?error=unknown", 302);
  }

  // Backend revoked every session and issued a fresh cookie; forward it so
  // the user stays logged in on the same browser.
  const headers = new Headers({ location: "/account?ok=password" });
  for (const c of res.headers.getSetCookie?.() ?? []) headers.append("set-cookie", c);
  return new Response(null, { status: 302, headers });
};
