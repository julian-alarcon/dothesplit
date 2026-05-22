import type { APIRoute } from "astro";

const internalBase =
  process.env.API_BASE_URL_INTERNAL ?? "http://localhost:8080";

// POST /api/reset: completes the password-reset flow. On 200 the backend
// returns a Set-Cookie session for the user; we forward it so they land on
// /groups already logged in.
export const POST: APIRoute = async ({ request, redirect }) => {
  const form = await request.formData();
  const email = (form.get("email") ?? "").toString().trim();
  const code = (form.get("code") ?? "").toString().trim();
  const new_password = (form.get("new_password") ?? "").toString();
  const password_confirmation = (form.get("password_confirmation") ?? "").toString();

  const back = (err: string) =>
    `/reset?email=${encodeURIComponent(email)}&error=${err}`;

  if (new_password.length < 10) return redirect(back("length"), 302);
  if (new_password !== password_confirmation) return redirect(back("mismatch"), 302);

  const res = await fetch(`${internalBase}/v1/auth/password-reset/confirm`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, code, new_password }),
  });
  if (res.status === 400) return redirect(back("invalid_code"), 302);
  if (res.status === 410) return redirect(back("code_expired"), 302);
  if (res.status === 429) return redirect(back("too_many_attempts"), 302);
  if (!res.ok) return redirect(back("unknown"), 302);

  const headers = new Headers({ location: "/groups?password_changed=1" });
  for (const c of res.headers.getSetCookie?.() ?? []) headers.append("set-cookie", c);
  return new Response(null, { status: 302, headers });
};
