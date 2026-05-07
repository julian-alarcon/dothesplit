import type { APIRoute } from "astro";

const internalBase =
  process.env.API_BASE_URL_INTERNAL ?? "http://localhost:8080";

export const POST: APIRoute = async ({ request, url, redirect }) => {
  const recurringID = url.searchParams.get("id");
  const groupID = url.searchParams.get("group");
  if (!recurringID || !groupID) return new Response("missing id", { status: 400 });

  const cookie = request.headers.get("cookie") ?? "";
  const res = await fetch(`${internalBase}/v1/recurring-expenses/${recurringID}`, {
    method: "DELETE",
    headers: { cookie },
  });
  if (!res.ok && res.status !== 204) {
    return redirect(`/groups/${groupID}/recurring?error=1`, 302);
  }
  return redirect(`/groups/${groupID}/recurring`, 302);
};
