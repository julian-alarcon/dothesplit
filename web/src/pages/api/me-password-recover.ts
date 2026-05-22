import type { APIRoute } from "astro";

const internalBase =
  process.env.API_BASE_URL_INTERNAL ?? "http://localhost:8080";

// POST /api/me-password-recover: invoked by the "Recover password by email"
// button on /account/password. Looks up the caller's own email, fires the
// password-reset request through the normal forgot-password backend path
// (so the user lands in the same /reset code-paste flow as a logged-out
// user would), then redirects to /reset with the email pre-filled.
export const POST: APIRoute = async ({ request, redirect }) => {
  const cookie = request.headers.get("cookie") ?? "";

  const meRes = await fetch(`${internalBase}/v1/me`, { headers: { cookie } });
  if (!meRes.ok) {
    return redirect("/login", 302);
  }
  const me = (await meRes.json()) as { email?: string };
  const email = me.email ?? "";
  if (!email) {
    return redirect("/account/password?error=unknown", 302);
  }

  // Fire-and-forget: backend always returns 204 to avoid enumeration.
  await fetch(`${internalBase}/v1/auth/password-reset/request`, {
    method: "POST",
    headers: { "Content-Type": "application/json", cookie },
    body: JSON.stringify({ email }),
  });

  return redirect(`/reset?email=${encodeURIComponent(email)}&from=account`, 302);
};
