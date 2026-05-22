import type { APIRoute } from "astro";

const internalBase =
  process.env.API_BASE_URL_INTERNAL ?? "http://localhost:8080";

// POST /api/admin/user-role: promote or demote a user. Step-up password
// required. Redirects back to the per-user detail page so the admin sees
// the result inline.
export const POST: APIRoute = async ({ request, redirect }) => {
  const form = await request.formData();
  const cookie = request.headers.get("cookie") ?? "";
  const id = (form.get("id") ?? "").toString();
  const role = (form.get("role") ?? "").toString();
  if (!id) return redirect("/admin/users?role_error=missing_id", 302);
  const detail = `/admin/users/${id}`;
  const res = await fetch(`${internalBase}/v1/admin/users/${id}/role`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json", cookie },
    body: JSON.stringify({
      role,
      password: (form.get("password") ?? "").toString(),
    }),
  });
  if (res.ok) return redirect(`${detail}?role=1`, 302);
  if (res.status === 401) return redirect(`${detail}?role_error=step_up`, 302);
  if (res.status === 423) return redirect(`${detail}?role_error=locked`, 302);
  if (res.status === 409) {
    const data = (await res.json().catch(() => ({}))) as { code?: string };
    if (data.code === "last_admin") return redirect(`${detail}?role_error=last_admin`, 302);
    if (data.code === "cannot_target_self") return redirect(`${detail}?role_error=self`, 302);
    return redirect(`${detail}?role_error=conflict`, 302);
  }
  return redirect(`${detail}?role_error=1`, 302);
};
