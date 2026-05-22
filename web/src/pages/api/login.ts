import type { APIRoute } from "astro";

const internalBase =
  process.env.API_BASE_URL_INTERNAL ?? "http://localhost:8080";

/**
 * Forwards the login form-post to the Go API and passes the Set-Cookie back
 * so the browser stores the session cookie on the Astro origin.
 *
 * 403 with code='email_unverified' means the account exists but hasn't
 * confirmed the email yet - redirect to /verify with a "resend" flag so the
 * user can pick up where they left off.
 */
export const POST: APIRoute = async ({ request, redirect }) => {
  const form = await request.formData();
  const email = form.get("email");
  const res = await fetch(`${internalBase}/v1/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      email,
      password: form.get("password"),
    }),
  });
  if (res.status === 403) {
    const body = await res.json().catch(() => ({}));
    if (body?.code === "email_unverified") {
      return redirect(
        `/verify?email=${encodeURIComponent(String(email ?? ""))}&resend=1`,
        302,
      );
    }
  }
  if (!res.ok) {
    return redirect("/login?error=1", 302);
  }
  const headers = new Headers();
  for (const c of res.headers.getSetCookie?.() ?? []) {
    headers.append("set-cookie", c);
  }
  headers.set("location", "/groups");
  return new Response(null, { status: 302, headers });
};
