import type { APIRoute } from "astro";

const internalBase =
  process.env.API_BASE_URL_INTERNAL ?? "http://localhost:8080";

type SplitPayload = { user_id: string; value?: number };

function parseSplitsJSON(
  raw: FormDataEntryValue | null,
): { mode: string; splits: SplitPayload[] } | null {
  if (!raw) return null;
  try {
    const parsed = JSON.parse(String(raw));
    if (typeof parsed?.mode !== "string" || !Array.isArray(parsed?.splits)) return null;
    const splits: SplitPayload[] = parsed.splits
      .filter((s: unknown): s is { user_id: string; value?: number } =>
        typeof (s as { user_id?: unknown })?.user_id === "string",
      )
      .map((s: { user_id: string; value?: number }) =>
        typeof s.value === "number" ? { user_id: s.user_id, value: s.value } : { user_id: s.user_id },
      );
    if (splits.length === 0) return null;
    return { mode: parsed.mode, splits };
  } catch {
    return null;
  }
}

export const POST: APIRoute = async ({ request, url, redirect }) => {
  const expenseID = url.searchParams.get("id");
  const groupID = url.searchParams.get("group");
  if (!expenseID || !groupID) return new Response("missing id", { status: 400 });

  const cookie = request.headers.get("cookie") ?? "";
  const form = await request.formData();

  const body: Record<string, unknown> = {};
  const description = (form.get("description") ?? "").toString().trim();
  if (description) body.description = description;
  const amount = (form.get("amount_dollars") ?? "").toString().trim();
  if (amount) body.amount_cents = Math.round(Number(amount) * 100);
  const categoryID = (form.get("category_id") ?? "").toString().trim();
  if (categoryID) body.category_id = categoryID;
  const payerID = (form.get("payer_id") ?? "").toString().trim();
  if (payerID) body.payer_id = payerID;
  const incurredAt = (form.get("incurred_at") ?? "").toString().trim();
  // Same noon-UTC anchor as the create flow.
  if (incurredAt && /^\d{4}-\d{2}-\d{2}$/.test(incurredAt)) {
    body.incurred_at = `${incurredAt}T12:00:00Z`;
  }

  const splits = parseSplitsJSON(form.get("splits_json"));
  if (splits) {
    body.mode = splits.mode;
    body.splits = splits.splits;
  }

  if (Object.keys(body).length === 0) {
    return redirect(`/groups/${groupID}/expenses/${expenseID}`, 302);
  }

  const res = await fetch(`${internalBase}/v1/expenses/${expenseID}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json", cookie },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    return redirect(`/groups/${groupID}/expenses/${expenseID}?error=1`, 302);
  }
  return redirect(`/groups/${groupID}/expenses/${expenseID}`, 302);
};
