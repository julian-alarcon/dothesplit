// Currency picker data. The "common" list is hand-curated (one-click for the
// 99% case); the rest come from Intl.supportedValuesOf("currency") so we
// don't maintain a 300-entry list by hand. currencyLabel() takes a locale
// so display names can localize via the request's Accept-Language.

export const COMMON_CURRENCIES = [
  "EUR", "USD", "GBP", "CHF", "CAD", "AUD", "JPY", "SEK", "NOK", "DKK",
  "COP", "MXN", "BRL", "INR", "CNY",
] as const;

const supportedFn = (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf;

export function allCurrencies(): string[] {
  if (typeof supportedFn !== "function") return [...COMMON_CURRENCIES];
  return supportedFn("currency");
}

export function otherCurrencies(): string[] {
  const common = new Set<string>(COMMON_CURRENCIES);
  return allCurrencies()
    .filter((c) => !common.has(c))
    .sort();
}

export function currencyLabel(code: string, locale: string): string {
  try {
    const name = new Intl.DisplayNames(locale, { type: "currency" }).of(code);
    return name && name !== code ? `${code} — ${name}` : code;
  } catch {
    return code;
  }
}
