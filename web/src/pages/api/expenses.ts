import type { APIRoute } from "astro";

const internalBase =
  process.env.API_BASE_URL_INTERNAL ?? "http://localhost:8080";

export const POST: APIRoute = async ({ request, url, redirect }) => {
  const groupID = url.searchParams.get("id");
  if (!groupID) return new Response("missing id", { status: 400 });
  const cookie = request.headers.get("cookie") ?? "";
  const form = await request.formData();

  const amountCents = Math.round(Number(form.get("amount_dollars") ?? "0") * 100);
  const payerID = String(form.get("payer_id") ?? "");
  const description = String(form.get("description") ?? "");
  const categoryID = (form.get("category_id") ?? "").toString().trim();
  const incurredAtRaw = (form.get("incurred_at") ?? "").toString().trim();

  const { mode, splits } = parseSplitsJSON(form.get("splits_json"));

  const body: Record<string, unknown> = {
    description,
    amount_cents: amountCents,
    payer_id: payerID,
    mode,
    splits,
  };
  if (categoryID) body.category_id = categoryID;
  // <input type="date"> emits "YYYY-MM-DD". Anchor at noon UTC to dodge
  // timezone edge cases that would push the displayed date a day off.
  if (incurredAtRaw && /^\d{4}-\d{2}-\d{2}$/.test(incurredAtRaw)) {
    body.incurred_at = `${incurredAtRaw}T12:00:00Z`;
  }

  await fetch(`${internalBase}/v1/groups/${groupID}/expenses`, {
    method: "POST",
    headers: { "Content-Type": "application/json", cookie },
    body: JSON.stringify(body),
  });
  return redirect(`/groups/${groupID}`, 302);
};

type SplitPayload = { user_id: string; value?: number };

function parseSplitsJSON(
  raw: FormDataEntryValue | null,
): { mode: string; splits: SplitPayload[] } {
  if (!raw) return { mode: "equal", splits: [] };
  try {
    const parsed = JSON.parse(String(raw));
    const mode = typeof parsed?.mode === "string" ? parsed.mode : "equal";
    const splits: SplitPayload[] = Array.isArray(parsed?.splits)
      ? parsed.splits
          .filter((s: unknown): s is { user_id: string; value?: number } =>
            typeof (s as { user_id?: unknown })?.user_id === "string",
          )
          .map((s: { user_id: string; value?: number }) =>
            typeof s.value === "number" ? { user_id: s.user_id, value: s.value } : { user_id: s.user_id },
          )
      : [];
    return { mode, splits };
  } catch {
    return { mode: "equal", splits: [] };
  }
}
