import type { APIRoute } from "astro";

const internalBase =
  process.env.API_BASE_URL_INTERNAL ?? "http://localhost:8080";

// POST /api/admin/user-delete: soft-delete a user. The form must include
// `password` (the admin's own) for step-up. On success the user is gone, so
// we redirect to the list. On failure we stay on the detail page so the
// admin can see the error in context.
export const POST: APIRoute = async ({ request, redirect }) => {
  const form = await request.formData();
  const cookie = request.headers.get("cookie") ?? "";
  const id = (form.get("id") ?? "").toString();
  if (!id) return redirect("/admin/users?delete_error=missing_id", 302);
  const detail = `/admin/users/${id}`;
  const res = await fetch(`${internalBase}/v1/admin/users/${id}`, {
    method: "DELETE",
    headers: { "Content-Type": "application/json", cookie },
    body: JSON.stringify({ password: (form.get("password") ?? "").toString() }),
  });
  if (res.status === 204) return redirect("/admin/users?deleted=1", 302);
  if (res.status === 401) return redirect(`${detail}?delete_error=step_up`, 302);
  if (res.status === 423) return redirect(`${detail}?delete_error=locked`, 302);
  if (res.status === 409) return redirect(`${detail}?delete_error=last_admin`, 302);
  return redirect(`${detail}?delete_error=1`, 302);
};
