import type { APIRoute } from "astro";

const internalBase =
  process.env.API_BASE_URL_INTERNAL ?? "http://localhost:8080";

// POST /api/admin/group-delete: admin-cascade-delete any group.
export const POST: APIRoute = async ({ request, redirect }) => {
  const form = await request.formData();
  const cookie = request.headers.get("cookie") ?? "";
  const id = (form.get("id") ?? "").toString();
  if (!id) return redirect("/admin/groups?delete_error=missing_id", 302);
  const res = await fetch(`${internalBase}/v1/admin/groups/${id}`, {
    method: "DELETE",
    headers: { "Content-Type": "application/json", cookie },
    body: JSON.stringify({ password: (form.get("password") ?? "").toString() }),
  });
  if (res.status === 204) return redirect("/admin/groups?deleted=1", 302);
  if (res.status === 401) return redirect("/admin/groups?delete_error=step_up", 302);
  if (res.status === 423) return redirect("/admin/groups?delete_error=locked", 302);
  return redirect("/admin/groups?delete_error=1", 302);
};
