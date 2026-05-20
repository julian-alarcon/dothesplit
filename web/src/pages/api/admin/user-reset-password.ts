import type { APIRoute } from "astro";

const internalBase =
  process.env.API_BASE_URL_INTERNAL ?? "http://localhost:8080";

// POST /api/admin/user-reset-password — scrambles the user's password hash,
// revokes every session, and emails them a 6-digit code so they can set a
// new password through /reset. Step-up password is the admin's own.
export const POST: APIRoute = async ({ request, redirect }) => {
  const form = await request.formData();
  const cookie = request.headers.get("cookie") ?? "";
  const id = (form.get("id") ?? "").toString();
  if (!id) return redirect("/admin/users?reset_error=missing_id", 302);
  const detail = `/admin/users/${id}`;
  const res = await fetch(`${internalBase}/v1/admin/users/${id}/password`, {
    method: "POST",
    headers: { "Content-Type": "application/json", cookie },
    body: JSON.stringify({
      password: (form.get("password") ?? "").toString(),
    }),
  });
  if (res.status === 204) return redirect(`${detail}?reset=1`, 302);
  if (res.status === 401) return redirect(`${detail}?reset_error=step_up`, 302);
  if (res.status === 423) return redirect(`${detail}?reset_error=locked`, 302);
  if (res.status === 503) return redirect(`${detail}?reset_error=smtp`, 302);
  return redirect(`${detail}?reset_error=1`, 302);
};
