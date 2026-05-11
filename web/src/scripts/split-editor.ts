// Split editor: opens a <dialog> that lets the user choose a split mode
// (equal / exact / percent) across a subset of group members. On submit
// it writes a JSON payload to a hidden input the parent form posts to the API.
//
// Wire-up: the editor lives inside a parent <form> and reads the amount from
// input[name="amount_dollars"] and the payer from select[name="payer_id"].
// On form submit, we re-validate and fill [name="splits_json"] so the SSR
// handler can forward {mode, splits:[{user_id, value?}]} to the Go API.

type Mode = "equal" | "exact" | "percent";

interface Member {
  id: string;
  name: string;
}

interface InitialSplit {
  user_id: string;
  share_cents: number;
}

function parseMembers(raw: string | undefined): Member[] {
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((m): m is Member => typeof m?.id === "string" && typeof m?.name === "string");
  } catch {
    return [];
  }
}

function parseInitial(raw: string | undefined): InitialSplit[] {
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(
      (s): s is InitialSplit => typeof s?.user_id === "string" && typeof s?.share_cents === "number",
    );
  } catch {
    return [];
  }
}

interface DefaultSplitEntry {
  user_id: string;
  basis_points: number;
}

function parseDefault(raw: string | undefined): DefaultSplitEntry[] {
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(
      (s): s is DefaultSplitEntry =>
        typeof s?.user_id === "string" && typeof s?.basis_points === "number",
    );
  } catch {
    return [];
  }
}

function formatCents(cents: number, currency: string): string {
  return new Intl.NumberFormat(undefined, {
    style: "currency",
    currency,
    currencyDisplay: "narrowSymbol",
  }).format(cents / 100);
}

// Drop trailing zeros (and the dot) from a fixed-2 decimal: "50.00" → "50", "33.30" → "33.3".
function trimZeros(s: string): string {
  return s.includes(".") ? s.replace(/\.?0+$/, "") : s;
}

// Render basis points as "50%" or "33.33%".
function formatPercent(bps: number): string {
  return `${trimZeros((bps / 100).toFixed(2))}%`;
}

// Equal split of `totalCents` across `n` members, distributing rounding
// remainder to the first (totalCents mod n) rows - matches the backend.
function equalShares(totalCents: number, n: number): number[] {
  if (n <= 0 || totalCents < 0) return [];
  const base = Math.floor(totalCents / n);
  const rem = totalCents - base * n;
  return Array.from({ length: n }, (_, i) => base + (i < rem ? 1 : 0));
}

function setupEditor(root: HTMLElement) {
  const dialog = root.querySelector<HTMLDialogElement>("[data-split-dialog]");
  const openBtn = root.querySelector<HTMLButtonElement>("[data-split-open]");
  const doneBtnEl = root.querySelector<HTMLButtonElement>("[data-split-done]");
  const cancelBtns = root.querySelectorAll<HTMLButtonElement>("[data-split-cancel]");
  const summaryEl = root.querySelector<HTMLElement>("[data-split-summary]");
  const payload = root.querySelector<HTMLInputElement>('input[name="splits_json"]');
  if (!dialog || !openBtn || !doneBtnEl || !summaryEl || !payload) return;
  const doneBtn = doneBtnEl;
  const summary = summaryEl;
  const payloadInput = payload;

  const form = root.closest<HTMLFormElement>("form");
  const amountInput = form?.querySelector<HTMLInputElement>('input[name="amount_dollars"]') ?? null;
  const payerSelect = form?.querySelector<HTMLSelectElement>('select[name="payer_id"]') ?? null;

  const members = parseMembers(root.dataset.members);
  const initial = parseInitial(root.dataset.initial);
  const groupDefault = parseDefault(root.dataset.default);
  const currency = root.dataset.currency ?? "EUR";
  const currentUserID = root.dataset.currentUser ?? "";

  // Build per-member row refs.
  const rows = members.map((m) => {
    const row = root.querySelector<HTMLElement>(`[data-split-row="${m.id}"]`);
    return {
      member: m,
      row: row!,
      checkbox: row!.querySelector<HTMLInputElement>('input[type="checkbox"]')!,
      valueInput: row!.querySelector<HTMLInputElement>("[data-split-value]")!,
      preview: row!.querySelector<HTMLElement>("[data-split-preview]")!,
      label: row!.querySelector<HTMLElement>("[data-split-label]")!,
    };
  });

  const totalDisplay = root.querySelector<HTMLElement>("[data-split-total]")!;
  const remainingDisplay = root.querySelector<HTMLElement>("[data-split-remaining]")!;
  const errorDisplay = root.querySelector<HTMLElement>("[data-split-error]")!;

  // State: a buffer edited inside the dialog; only committed to the form on "Done".
  type RowState = { userID: string; included: boolean; value: number };
  let mode: Mode = "equal";
  let state: RowState[] = [];
  // True after the user confirms a split via "Done". While false, the parent
  // form posts without `splits_json`, so the backend keeps the existing split
  // (rescaled proportionally if amount changed) - matching legacy behavior.
  let dirty = false;

  function readAmountCents(): number {
    if (!amountInput) return 0;
    const v = Number(amountInput.value);
    if (!Number.isFinite(v) || v <= 0) return 0;
    return Math.round(v * 100);
  }

  // hasUsableDefault: the group has a 2-entry default and both entries refer
  // to current members. (Belt-and-suspenders - backend already auto-clears
  // when membership grows past 2, but the UI shouldn't blow up otherwise.)
  function hasUsableDefault(): boolean {
    if (groupDefault.length !== 2 || members.length !== 2) return false;
    const memberIDs = new Set(members.map((m) => m.id));
    return groupDefault.every((e) => memberIDs.has(e.user_id));
  }

  function initFromInitial() {
    if (initial.length === 0) {
      if (hasUsableDefault()) {
        const byID = new Map(groupDefault.map((e) => [e.user_id, e.basis_points]));
        state = members.map((m) => ({
          userID: m.id,
          included: true,
          value: byID.get(m.id) ?? 0,
        }));
        mode = "percent";
        return;
      }
      state = members.map((m) => ({ userID: m.id, included: true, value: 0 }));
      mode = "equal";
      return;
    }
    const byID = new Map(initial.map((s) => [s.user_id, s.share_cents]));
    state = members.map((m) => ({
      userID: m.id,
      included: byID.has(m.id),
      value: byID.get(m.id) ?? 0,
    }));
    // Best default when opening an existing split is "exact" - losslessly
    // round-trips stored cents. The user can switch to percent/equal.
    mode = "exact";
  }

  // When a mode switch happens, prefill values to a sane baseline so the user
  // can tweak instead of starting at zero.
  function prefillForMode() {
    const amount = readAmountCents();
    const included = state.filter((s) => s.included);
    if (mode === "equal") {
      for (const s of state) s.value = 0;
      return;
    }
    if (included.length === 0) return;
    if (mode === "exact") {
      const shares = equalShares(amount, included.length);
      let i = 0;
      for (const s of state) s.value = s.included ? shares[i++] ?? 0 : 0;
      return;
    }
    // percent - basis points, evenly split, remainder to first rows.
    const base = Math.floor(10000 / included.length);
    const rem = 10000 - base * included.length;
    let i = 0;
    for (const s of state) {
      if (!s.included) {
        s.value = 0;
        continue;
      }
      s.value = base + (i < rem ? 1 : 0);
      i++;
    }
  }

  function computedShares(amount: number): Map<string, number> {
    const out = new Map<string, number>();
    const included = state.filter((s) => s.included);
    if (included.length === 0) return out;
    if (mode === "equal") {
      const shares = equalShares(amount, included.length);
      let i = 0;
      for (const s of state) {
        if (s.included) out.set(s.userID, shares[i++] ?? 0);
      }
      return out;
    }
    if (mode === "exact") {
      for (const s of state) if (s.included) out.set(s.userID, s.value);
      return out;
    }
    // percent
    let assigned = 0;
    for (const s of state) {
      if (!s.included) continue;
      const share = Math.floor((amount * s.value) / 10000);
      out.set(s.userID, share);
      assigned += share;
    }
    let i = 0;
    const includedList = state.filter((s) => s.included);
    while (assigned < amount && includedList.length > 0) {
      const target = includedList[i % includedList.length];
      out.set(target.userID, (out.get(target.userID) ?? 0) + 1);
      assigned++;
      i++;
    }
    return out;
  }

  const rowsByID = new Map(rows.map((r) => [r.member.id, r]));
  const memberByID = new Map(members.map((m) => [m.id, m]));

  function twoPersonLabel(userID: string): string {
    // Only used when exactly 2 members are *in the group* (state length 2).
    // Frames the row as "you owe X" / "X owes you" relative to the payer.
    const m = memberByID.get(userID);
    const fallback = m?.name ?? userID;
    if (!currentUserID || state.length !== 2) return fallback;
    const payerID = payerSelect?.value ?? "";
    if (userID === payerID) return `${fallback} paid`;
    if (payerID === currentUserID) return `${fallback} owes you`;
    if (userID === currentUserID) {
      const other = memberByID.get(payerID);
      return other ? `You owe ${other.name}` : fallback;
    }
    return fallback;
  }

  function render() {
    const amount = readAmountCents();
    const shares = computedShares(amount);
    const focused = document.activeElement;
    for (const s of state) {
      const r = rowsByID.get(s.userID);
      if (!r) continue;
      r.checkbox.checked = s.included;
      r.valueInput.disabled = !s.included || mode === "equal";
      // Equal mode has no per-row value to enter; hide the input but keep its
      // layout slot so the dialog height doesn't jump when switching modes.
      r.valueInput.style.visibility = mode === "equal" ? "hidden" : "";
      // Don't overwrite the field the user is currently typing into - that
      // resets the caret to the end and corrupts mid-edit selection.
      if (r.valueInput !== focused) {
        if (mode === "equal") {
          r.valueInput.value = "";
        } else if (mode === "exact") {
          r.valueInput.value = s.included ? (s.value / 100).toFixed(2) : "";
        } else {
          // Percent: hide trailing .00 (50, not 50.00); keep cents when needed.
          r.valueInput.value = s.included ? trimZeros((s.value / 100).toFixed(2)) : "";
        }
      }
      const share = shares.get(s.userID) ?? 0;
      r.preview.textContent = s.included ? formatCents(share, currency) : "-";
      r.label.textContent = twoPersonLabel(s.userID);
    }

    let sumDisplay = "";
    let remainingText = "";
    let valid = true;
    if (mode === "equal") {
      sumDisplay = formatCents(amount, currency);
      remainingText = "";
    } else if (mode === "exact") {
      let sum = 0;
      for (const s of state) if (s.included) sum += s.value;
      sumDisplay = `${formatCents(sum, currency)} / ${formatCents(amount, currency)}`;
      const remaining = amount - sum;
      remainingText = remaining === 0 ? "" : `Remaining: ${formatCents(remaining, currency)}`;
      valid = sum === amount && amount > 0 && state.some((s) => s.included);
    } else {
      let bps = 0;
      for (const s of state) if (s.included) bps += s.value;
      sumDisplay = `${formatPercent(bps)} / 100%`;
      const remaining = 10000 - bps;
      remainingText = remaining === 0 ? "" : `Remaining: ${formatPercent(remaining)}`;
      valid = bps === 10000 && amount > 0 && state.some((s) => s.included);
    }
    totalDisplay.textContent = sumDisplay;
    remainingDisplay.textContent = remainingText;

    if (!state.some((s) => s.included)) {
      errorDisplay.textContent = "Select at least one member.";
      valid = false;
    } else if (amount <= 0) {
      errorDisplay.textContent = "Enter an amount first.";
      valid = false;
    } else {
      errorDisplay.textContent = "";
    }

    doneBtn.disabled = !valid;
  }

  function renderSummary() {
    const amount = readAmountCents();
    const shares = computedShares(amount);
    const parts: string[] = [];
    for (const s of state) {
      if (!s.included) continue;
      const share = shares.get(s.userID) ?? 0;
      const name = memberByID.get(s.userID)?.name ?? s.userID;
      parts.push(`${name}: ${formatCents(share, currency)}`);
    }
    summary.textContent =
      mode === "equal" ? `Split equally between ${state.filter((s) => s.included).length} member(s)` : parts.join(" · ");
  }

  function commit() {
    const payload = {
      mode,
      splits: state
        .filter((s) => s.included)
        .map((s) => {
          if (mode === "equal") return { user_id: s.userID };
          return { user_id: s.userID, value: s.value };
        }),
    };
    payloadInput.value = JSON.stringify(payload);
    // Programmatic value writes don't fire input/change events, so the
    // dirty-form watcher (web/src/scripts/dirty-form.ts) wouldn't enable the
    // Save button on edit forms. Dispatch them explicitly.
    payloadInput.dispatchEvent(new Event("input", { bubbles: true }));
    payloadInput.dispatchEvent(new Event("change", { bubbles: true }));
    dirty = true;
    renderSummary();
  }

  // Event wiring.
  const modeInputs = root.querySelectorAll<HTMLInputElement>('input[name="split_mode"]');
  modeInputs.forEach((input) => {
    input.addEventListener("change", () => {
      if (!input.checked) return;
      mode = input.value as Mode;
      prefillForMode();
      render();
    });
  });

  rows.forEach((r) => {
    r.checkbox.addEventListener("change", () => {
      const s = state.find((x) => x.userID === r.member.id);
      if (!s) return;
      s.included = r.checkbox.checked;
      prefillForMode();
      render();
    });
    r.valueInput.addEventListener("input", () => {
      const s = state.find((x) => x.userID === r.member.id);
      if (!s) return;
      const n = Number(r.valueInput.value);
      if (!Number.isFinite(n) || n < 0) {
        s.value = 0;
      } else {
        // exact = cents, percent = basis points (both via 2-decimal x100).
        s.value = Math.round(n * 100);
      }
      render();
    });
  });

  openBtn.addEventListener("click", (e) => {
    e.preventDefault();
    // Don't reinitialize state here - it's seeded once at setup below.
    // Reopening must preserve whatever the user last committed via Done;
    // calling initFromInitial() again would reset to the original/default split.
    // Sync mode radio with state in case the DOM drifted from `mode`.
    modeInputs.forEach((i) => (i.checked = i.value === mode));
    render();
    dialog.showModal();
    // Focus the active mode radio so keyboard / screen-reader users land
    // inside the modal instead of on <body>. Defer until showModal lays out.
    queueMicrotask(() => {
      const active = root.querySelector<HTMLInputElement>(
        `input[name="split_mode"][value="${mode}"]`,
      );
      active?.focus();
    });
  });

  cancelBtns.forEach((btn) => {
    btn.addEventListener("click", (e) => {
      e.preventDefault();
      dialog.close();
    });
  });

  // No backdrop-close listener on purpose - pointer cancellation (WCAG 2.5.7),
  // matching the DatePicker dialog. Escape still closes via native <dialog>.

  doneBtn.addEventListener("click", (e) => {
    e.preventDefault();
    if (doneBtn.disabled) return;
    commit();
    dialog.close();
  });

  // When amount or payer change in the parent form, keep the preview in sync.
  amountInput?.addEventListener("input", () => {
    // Re-prefill in equal/percent only; exact preserves the user's typed cents.
    if (mode !== "exact") prefillForMode();
    render();
  });
  payerSelect?.addEventListener("change", render);

  // For edit flows (initialSplits provided), an untouched editor leaves
  // splits_json empty so the backend keeps the existing split (or rescales
  // proportionally on amount change). For create flows, always emit a payload
  // so the API gets at least one split.
  const hasInitial = initial.length > 0;
  form?.addEventListener("submit", () => {
    if (!dirty && hasInitial) payloadInput.value = "";
  });

  // Initial render + auto-commit so create flows have a valid payload ready.
  initFromInitial();
  // Don't prefill if initFromInitial already loaded values (from initialSplits
  // or from the group's default_split) - that would clobber them.
  if (!hasInitial && !hasUsableDefault()) prefillForMode();
  if (hasInitial) {
    renderSummary();
  } else {
    commit();
    dirty = false;
  }
}

document.querySelectorAll<HTMLElement>("[data-split-editor]").forEach(setupEditor);
