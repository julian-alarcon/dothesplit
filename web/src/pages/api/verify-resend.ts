import type { APIRoute } from "astro";

const internalBase =
  process.env.API_BASE_URL_INTERNAL ?? "http://localhost:8080";

export const POST: APIRoute = async ({ request, redirect }) => {
  const form = await request.formData();
  const email = String(form.get("email") ?? "");
  await fetch(`${internalBase}/v1/auth/verify/resend`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email }),
  });
  // Always redirect with a friendly notice - the API also always returns
  // 204 to avoid account enumeration.
  return redirect(
    `/verify?email=${encodeURIComponent(email)}&resent=1`,
    302,
  );
};
