import type { APIRoute } from "astro";

const internalBase =
  process.env.API_BASE_URL_INTERNAL ?? "http://localhost:8080";

export const POST: APIRoute = async ({ request, url, redirect }) => {
  const groupID = url.searchParams.get("id");
  if (!groupID) return new Response("missing id", { status: 400 });

  const form = await request.formData();
  const cookie = request.headers.get("cookie") ?? "";

  const body: Record<string, unknown> = {};
  const name = (form.get("name") ?? "").toString().trim();
  if (name) body.name = name;
  const currency = (form.get("default_currency") ?? "").toString().trim();
  if (currency) body.default_currency = currency.toUpperCase();

  if (Object.keys(body).length === 0) {
    return redirect(`/groups/${groupID}/settings`, 302);
  }

  const res = await fetch(`${internalBase}/v1/groups/${groupID}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json", cookie },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    if (res.status === 409) {
      const reason = "Currency is locked once the group has expenses or settlements.";
      return redirect(
        `/groups/${groupID}/settings?error=1&reason=${encodeURIComponent(reason)}`,
        302,
      );
    }
    return redirect(`/groups/${groupID}/settings?error=1`, 302);
  }
  return redirect(`/groups/${groupID}/settings`, 302);
};
