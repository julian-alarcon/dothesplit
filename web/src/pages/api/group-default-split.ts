import type { APIRoute } from "astro";

const internalBase =
  process.env.API_BASE_URL_INTERNAL ?? "http://localhost:8080";

export const POST: APIRoute = async ({ request, url, redirect }) => {
  const groupID = url.searchParams.get("id");
  if (!groupID) return new Response("missing id", { status: 400 });

  const form = await request.formData();
  const cookie = request.headers.get("cookie") ?? "";

  const action = String(form.get("action") ?? "set");
  let body: Record<string, unknown>;
  if (action === "clear") {
    body = { default_split: [] };
  } else {
    // The form posts member_id_1, percent_1, member_id_2, percent_2.
    // Percent comes in as a 0..100 number; we convert to basis points.
    const entries: { user_id: string; basis_points: number }[] = [];
    for (let i = 1; i <= 2; i++) {
      const id = String(form.get(`member_id_${i}`) ?? "").trim();
      const pctRaw = String(form.get(`percent_${i}`) ?? "").trim();
      const pct = Number(pctRaw);
      if (!id || !Number.isFinite(pct)) {
        return redirect(`/groups/${groupID}/settings?error=1`, 302);
      }
      entries.push({ user_id: id, basis_points: Math.round(pct * 100) });
    }
    body = { default_split: entries };
  }

  const res = await fetch(`${internalBase}/v1/groups/${groupID}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json", cookie },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    return redirect(`/groups/${groupID}/settings?error=1`, 302);
  }
  return redirect(`/groups/${groupID}/settings`, 302);
};
