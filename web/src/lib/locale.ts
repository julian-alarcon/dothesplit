// Resolve a BCP-47 locale tag from an Accept-Language header. Returns the
// highest-q-value tag the runtime can honor (i.e. has CLDR data for), with
// a hard fallback to "en-US". Designed so future i18n work can layer a
// stored per-user override on top - mirrors the 3-tier shape used by
// timezone resolution in lib/timezone.ts.

function isUsable(tag: string): boolean {
  try {
    return Intl.DateTimeFormat.supportedLocalesOf([tag]).length > 0;
  } catch {
    return false;
  }
}

export function resolveLocale(acceptLanguage: string | null): string {
  if (!acceptLanguage) return "en-US";
  const candidates = acceptLanguage
    .split(",")
    .map((part) => {
      const [tag, ...params] = part.trim().split(";");
      const qParam = params.find((p) => p.trim().startsWith("q="));
      const q = qParam ? Number(qParam.trim().slice(2)) : 1;
      return { tag: tag.trim(), q };
    })
    .filter((c) => c.tag && c.tag !== "*" && Number.isFinite(c.q))
    .sort((a, b) => b.q - a.q);
  for (const { tag } of candidates) {
    if (isUsable(tag)) return tag;
  }
  return "en-US";
}
