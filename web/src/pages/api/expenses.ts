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
  const cadence = (form.get("cadence") ?? "").toString().trim();

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
  let incurredAtISO: string | null = null;
  if (incurredAtRaw && /^\d{4}-\d{2}-\d{2}$/.test(incurredAtRaw)) {
    incurredAtISO = `${incurredAtRaw}T12:00:00Z`;
    body.incurred_at = incurredAtISO;
  }

  await fetch(`${internalBase}/v1/groups/${groupID}/expenses`, {
    method: "POST",
    headers: { "Content-Type": "application/json", cookie },
    body: JSON.stringify(body),
  });

  // If a cadence was selected, also create a recurring template anchored at
  // the next occurrence (so the worker materializes the *second* one on its
  // tick — the first occurrence is the expense we just POSTed above).
  if (isValidCadence(cadence) && incurredAtISO) {
    const nextRunAt = advanceCadence(incurredAtISO, cadence);
    const recurringBody: Record<string, unknown> = {
      description,
      amount_cents: amountCents,
      payer_id: payerID,
      mode,
      splits,
      cadence,
      next_run_at: nextRunAt,
    };
    if (categoryID) recurringBody.category_id = categoryID;
    await fetch(`${internalBase}/v1/groups/${groupID}/recurring-expenses`, {
      method: "POST",
      headers: { "Content-Type": "application/json", cookie },
      body: JSON.stringify(recurringBody),
    });
  }

  return redirect(`/groups/${groupID}`, 302);
};

function isValidCadence(c: string): c is "daily" | "weekly" | "biweekly" | "monthly" | "yearly" {
  return c === "daily" || c === "weekly" || c === "biweekly" || c === "monthly" || c === "yearly";
}

// Mirrors api/internal/service/recurring.go advanceCadence so the SSR handler
// can compute the next run without an extra round-trip. Operates on an ISO
// date-time string and returns the same format.
function advanceCadence(fromISO: string, cadence: string): string {
  const d = new Date(fromISO);
  switch (cadence) {
    case "daily":
      d.setUTCDate(d.getUTCDate() + 1);
      break;
    case "weekly":
      d.setUTCDate(d.getUTCDate() + 7);
      break;
    case "biweekly":
      d.setUTCDate(d.getUTCDate() + 14);
      break;
    case "monthly":
      d.setUTCMonth(d.getUTCMonth() + 1);
      break;
    case "yearly":
      d.setUTCFullYear(d.getUTCFullYear() + 1);
      break;
  }
  return d.toISOString();
}

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
