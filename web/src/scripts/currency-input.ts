// Currency-aware input. Wires a visible <input data-currency-input> to a
// hidden <input> that the form actually submits as the numeric amount.
//
// While focused: shows the raw editable number (e.g. "1234.5") so typing
// stays simple and decimals/thousands separators don't fight the user.
// On blur (and on initial render): replaces the visible value with a fully
// formatted localized string (e.g. "1,234.50 €") using the field's currency.
// The hidden input always carries the canonical "1234.50" form.

interface SetupOptions {
  visible: HTMLInputElement;
  hidden: HTMLInputElement;
  currency: string;
}

function parse(raw: string): number | null {
  if (!raw) return null;
  // Accept both "1,234.56" and "1.234,56" pasted in - normalize to a dot
  // decimal. Heuristic: the *last* non-digit separator wins as the decimal.
  const cleaned = raw.replace(/[^0-9.,-]/g, "");
  if (!cleaned) return null;
  const lastDot = cleaned.lastIndexOf(".");
  const lastComma = cleaned.lastIndexOf(",");
  let normalized = cleaned;
  if (lastDot !== -1 && lastComma !== -1) {
    if (lastComma > lastDot) {
      normalized = cleaned.replace(/\./g, "").replace(",", ".");
    } else {
      normalized = cleaned.replace(/,/g, "");
    }
  } else if (lastComma !== -1) {
    normalized = cleaned.replace(/\./g, "").replace(",", ".");
  } else {
    normalized = cleaned.replace(/,/g, "");
  }
  const n = Number(normalized);
  return Number.isFinite(n) ? n : null;
}

function format(n: number, currency: string): string {
  return new Intl.NumberFormat(undefined, {
    style: "currency",
    currency,
    currencyDisplay: "narrowSymbol",
  }).format(n);
}

function rawForEdit(n: number): string {
  // Trim trailing .00 → "1234"; keep "1234.5" / "1234.56" as-is.
  const fixed = n.toFixed(2);
  return fixed.replace(/\.?0+$/, "");
}

function setupCurrencyInput({ visible, hidden, currency }: SetupOptions) {
  function setHidden(next: string) {
    if (hidden.value === next) return;
    hidden.value = next;
    // Programmatic value writes don't fire input/change automatically.
    // Dispatch both: SplitEditor listens to "input" to recompute live shares,
    // dirty-form re-snapshots on either, and any future listener can pick
    // whichever it prefers. Matches what category-picker / split-editor /
    // date-picker do after their own commits.
    hidden.dispatchEvent(new Event("input", { bubbles: true }));
    hidden.dispatchEvent(new Event("change", { bubbles: true }));
  }

  function commit(): number | null {
    const n = parse(visible.value);
    if (n === null || n <= 0) {
      setHidden("");
      return null;
    }
    setHidden(n.toFixed(2));
    return n;
  }

  visible.addEventListener("focus", () => {
    const n = parse(visible.value);
    if (n !== null) visible.value = rawForEdit(n);
    visible.select();
  });

  visible.addEventListener("input", () => {
    commit();
  });

  visible.addEventListener("blur", () => {
    const n = commit();
    visible.value = n === null ? "" : format(n, currency);
  });

  // If the field starts with a value (edit flows), render it formatted.
  const initial = parse(visible.value);
  if (initial !== null) {
    hidden.value = initial.toFixed(2);
    visible.value = format(initial, currency);
  }

  // Re-format if the parent form's currency dropdown changes after init.
  const form = visible.closest("form");
  form?.addEventListener("submit", () => {
    commit();
  });
}

document.querySelectorAll<HTMLInputElement>("[data-currency-input]").forEach((visible) => {
  const hiddenName = visible.dataset.currencyHidden;
  if (!hiddenName) return;
  const form = visible.closest("form");
  const hidden = form?.querySelector<HTMLInputElement>(`input[type="hidden"][name="${hiddenName}"]`);
  if (!hidden) return;
  const currency = visible.dataset.currency || "EUR";
  setupCurrencyInput({ visible, hidden, currency });
});

export {};
