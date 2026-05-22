import type { APIRoute } from "astro";

const internalBase =
  process.env.API_BASE_URL_INTERNAL ?? "http://localhost:8080";

// POST /api/forgot: fires the password-reset request and redirects to
// /reset. Always redirects to /reset whether or not the email is known
// (the backend already returns 204 unconditionally for enumeration safety,
// the SSR layer just keeps the same UX on top).
export const POST: APIRoute = async ({ request, redirect }) => {
  const form = await request.formData();
  const email = (form.get("email") ?? "").toString().trim();
  if (!email) return redirect("/forgot?error=1", 302);

  await fetch(`${internalBase}/v1/auth/password-reset/request`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email }),
  });

  return redirect(`/reset?email=${encodeURIComponent(email)}`, 302);
};
