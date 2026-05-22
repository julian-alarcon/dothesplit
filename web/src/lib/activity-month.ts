// Helpers for the "Month YYYY" dividers that group the activity feed by
// calendar month. The activity API returns items newest-first, so when the
// month flips we emit a header before the next item. Pagination cooperates by
// emitting a header at the *start* of every fragment too; the client
// deduplicates if the first fragment header matches the last one already on
// the page (keeps the rendering self-contained per fragment).

export type MonthHeader = {
  kind: "month-header";
  /** "YYYY-MM" - used by the client to dedupe across fragments. */
  key: string;
  /** Localized "Month YYYY" label, ready to render. */
  label: string;
};

export type ActivityRow<T> =
  | MonthHeader
  | { kind: "item"; item: T };

function monthKey(d: Date): string {
  return `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, "0")}`;
}

/**
 * Walk the items in feed order and yield interleaved month headers.
 * `getOccurredAt` returns an RFC3339 string per item.
 */
export function withMonthHeaders<T>(
  items: T[],
  getOccurredAt: (item: T) => string,
  locale: string,
): ActivityRow<T>[] {
  if (items.length === 0) return [];
  const fmt = new Intl.DateTimeFormat(locale, {
    month: "long",
    year: "numeric",
    timeZone: "UTC",
  });
  const out: ActivityRow<T>[] = [];
  let lastKey: string | null = null;
  for (const item of items) {
    const d = new Date(getOccurredAt(item));
    const key = monthKey(d);
    if (key !== lastKey) {
      out.push({ kind: "month-header", key, label: fmt.format(d) });
      lastKey = key;
    }
    out.push({ kind: "item", item });
  }
  return out;
}
