// Currency picker data. The "common" list is hand-curated (one-click for the
// 99% case); the rest come from Intl.supportedValuesOf("currency") so we
// don't maintain a 300-entry list by hand. currencyLabel() takes a locale
// so display names can localize via the request's Accept-Language.

export const COMMON_CURRENCIES = [
  "EUR", "USD", "GBP", "CHF", "CAD", "AUD", "JPY", "SEK", "NOK", "DKK",
  "COP", "MXN", "BRL", "INR", "CNY", "ILS",
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

// ISO-4217 codes whose first two letters are not a valid ISO-3166 country
// (mostly supranational / metal / fund codes). Anything not listed falls
// through to the generic "first two letters → flag" rule, which works for
// the vast majority of national currencies (USD → US, GBP → GB, etc.).
const FLAG_OVERRIDES: Record<string, string> = {
  EUR: "🇪🇺",
  ILS: "🇵🇸", // 🍉
  XCD: "🌎", // East Caribbean dollar
  XOF: "🌍", // West African CFA franc
  XAF: "🌍", // Central African CFA franc
  XPF: "🌏", // CFP franc (Pacific)
  XAU: "🥇", // gold
  XAG: "🥈", // silver
  XPT: "⚪", // platinum
  XPD: "⚪", // palladium
  XDR: "🏦", // IMF special drawing rights
  XSU: "🏦",
  XUA: "🏦",
  XBA: "🏦",
  XBB: "🏦",
  XBC: "🏦",
  XBD: "🏦",
  XTS: "🧪", // testing code
  XXX: "❔", // no currency
};

export function currencyFlag(code: string): string {
  const override = FLAG_OVERRIDES[code];
  if (override) return override;
  // Convert "US" / "GB" / etc. to regional-indicator pair (🇺🇸, 🇬🇧).
  // Each ASCII letter offset by 0x1F1E6 - "A".charCodeAt(0) = 127397.
  const cc = code.slice(0, 2).toUpperCase();
  if (cc.length !== 2 || !/^[A-Z]{2}$/.test(cc)) return "💱";
  return String.fromCodePoint(...[...cc].map((ch) => ch.charCodeAt(0) + 127397));
}

const NAME_OVERRIDES: Record<string, string> = {
  ILS: "Palestine Shekel",
};

// Cache one Intl.DisplayNames instance per locale. The currency picker
// renders ~170 options on the "All currencies" optgroup, so reusing the
// formatter avoids re-parsing the locale ICU data on every label.
const displayNamesCache = new Map<string, Intl.DisplayNames | null>();
function getDisplayNames(locale: string): Intl.DisplayNames | null {
  if (displayNamesCache.has(locale)) return displayNamesCache.get(locale) ?? null;
  let dn: Intl.DisplayNames | null = null;
  try {
    dn = new Intl.DisplayNames(locale, { type: "currency" });
  } catch {
    dn = null;
  }
  displayNamesCache.set(locale, dn);
  return dn;
}

export function currencyLabel(code: string, locale: string): string {
  const flag = currencyFlag(code);
  const override = NAME_OVERRIDES[code];
  if (override) return `${flag} ${code} - ${override}`;
  const dn = getDisplayNames(locale);
  if (!dn) return `${flag} ${code}`;
  const name = dn.of(code);
  return name && name !== code ? `${flag} ${code} - ${name}` : `${flag} ${code}`;
}
