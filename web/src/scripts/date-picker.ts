// Date picker: opens a <dialog> with a custom month-grid calendar. The dialog
// hosts a <form method="dialog">, so submit buttons close the dialog with
// `dialog.returnValue` set to the button's `value` ("done" or "cancel").
// Escape also fires submit with no value — treated as cancel.
//
// Day clicks set a *pending* selection without committing. Only "Done"
// commits: writes the picked YYYY-MM-DD to the hidden input the parent form
// posts and dispatches a `change` event so dirty-form etc. react.
//
// Backdrop clicks and clicks inside <select> options do NOT close the dialog
// (WCAG 2.5.7 pointer cancellation, and a Firefox quirk that fires fake
// clicks on the dialog when a <select> option is picked).

const labelFmt = new Intl.DateTimeFormat(undefined, {
  weekday: "short",
  year: "numeric",
  month: "short",
  day: "numeric",
});
const longDateFmt = new Intl.DateTimeFormat(undefined, {
  weekday: "long",
  year: "numeric",
  month: "long",
  day: "numeric",
});
const monthTitleFmt = new Intl.DateTimeFormat(undefined, {
  month: "long",
  year: "numeric",
});
const weekdayFmt = new Intl.DateTimeFormat(undefined, { weekday: "short" });

function formatLabel(yyyymmdd: string): string {
  // Anchor at noon UTC to match what the SSR handler does on submit, so the
  // displayed weekday matches what the server will store.
  const d = new Date(`${yyyymmdd}T12:00:00Z`);
  return Number.isNaN(d.getTime()) ? yyyymmdd : labelFmt.format(d);
}

function formatLongDate(yyyymmdd: string): string {
  const d = new Date(`${yyyymmdd}T12:00:00Z`);
  return Number.isNaN(d.getTime()) ? yyyymmdd : longDateFmt.format(d);
}

function todayISO(): string {
  return new Date().toISOString().slice(0, 10);
}

function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n);
}

function isoFromYMD(y: number, m: number, d: number): string {
  return `${y}-${pad2(m + 1)}-${pad2(d)}`;
}

function parseISO(iso: string): { y: number; m: number; d: number } {
  const [ys, ms, ds] = iso.split("-");
  return { y: Number(ys), m: Number(ms) - 1, d: Number(ds) };
}

// Add `days` to a YYYY-MM-DD string, returning YYYY-MM-DD.
function addDays(iso: string, days: number): string {
  const { y, m, d } = parseISO(iso);
  const dt = new Date(Date.UTC(y, m, d));
  dt.setUTCDate(dt.getUTCDate() + days);
  return dt.toISOString().slice(0, 10);
}

function setupPicker(root: HTMLElement) {
  const dialog = root.querySelector<HTMLDialogElement>("[data-date-dialog]");
  const openBtn = root.querySelector<HTMLButtonElement>("[data-date-open]");
  const hidden = root.querySelector<HTMLInputElement>("[data-date-value]");
  const label = root.querySelector<HTMLElement>("[data-date-label]");
  const dayBadge = root.querySelector<HTMLElement>("[data-date-day]");
  const todayBtn = root.querySelector<HTMLButtonElement>("[data-date-today]");
  const doneBtn = root.querySelector<HTMLButtonElement>("[data-date-done]");
  const cancelBtns = root.querySelectorAll<HTMLButtonElement>("[data-date-cancel]");
  const calTitle = root.querySelector<HTMLElement>("[data-cal-title]");
  const calPrev = root.querySelector<HTMLButtonElement>("[data-cal-prev]");
  const calNext = root.querySelector<HTMLButtonElement>("[data-cal-next]");
  const calWeekdays = root.querySelector<HTMLElement>("[data-cal-weekdays]");
  const calGrid = root.querySelector<HTMLElement>("[data-cal-grid]");
  if (
    !dialog || !openBtn || !hidden || !dayBadge || !todayBtn || !doneBtn ||
    !calTitle || !calPrev || !calNext || !calWeekdays || !calGrid
  ) return;

  // 0 = Sunday-first, 1 = Monday-first. Sourced from the data attribute
  // (which the .astro reads from Astro.locals.user.week_start).
  const weekStart = root.dataset.weekStart === "0" ? 0 : 1;

  // Cadence wiring (only present when the parent passed cadenceName).
  const cadenceHidden = root.querySelector<HTMLInputElement>("[data-cadence-value]");
  const cadenceSelect = root.querySelector<HTMLSelectElement>("[data-cadence-select]");
  const cadenceBadge = root.querySelector<HTMLElement>("[data-cadence-badge]");
  const cadenceLabels: Record<string, string> = {
    daily: "Repeats daily",
    weekly: "Repeats weekly",
    biweekly: "Repeats every 2 weeks",
    monthly: "Repeats monthly",
    yearly: "Repeats yearly",
  };

  // Initial state. `selected` mirrors the committed value (hidden input);
  // `pending` is what the user is currently looking at inside the modal but
  // has not yet committed. They diverge between open and Done/Cancel.
  let pending: string = hidden.value || todayISO();
  let pendingCadence: string = cadenceHidden?.value ?? "";
  let viewYear = parseISO(pending).y;
  let viewMonth = parseISO(pending).m;

  const renderTrigger = (iso: string) => {
    if (label) label.textContent = formatLabel(iso);
    const day = Number(iso.slice(8, 10));
    if (Number.isFinite(day) && day > 0) dayBadge.textContent = String(day);
  };

  const renderCadenceBadge = (cadence: string) => {
    if (!cadenceBadge) return;
    const text = cadenceLabels[cadence] ?? "";
    if (text) {
      cadenceBadge.textContent = text;
      cadenceBadge.classList.remove("hidden");
    } else {
      cadenceBadge.textContent = "";
      cadenceBadge.classList.add("hidden");
    }
  };

  // Render the weekday header row honouring weekStart.
  const renderWeekdays = () => {
    calWeekdays.replaceChildren();
    // Anchor: Sunday 2024-01-07 (UTC). Add weekStart days to start at Mon.
    for (let i = 0; i < 7; i++) {
      const d = new Date(Date.UTC(2024, 0, 7 + ((i + weekStart) % 7)));
      const cell = document.createElement("div");
      cell.textContent = weekdayFmt.format(d).slice(0, 2);
      calWeekdays.appendChild(cell);
    }
  };

  // Render the day grid using `pending` (not `hidden.value`) for highlighting.
  // Returns the focusable button for the pending day so callers can focus it.
  const renderGrid = (): HTMLButtonElement | null => {
    calTitle.textContent = monthTitleFmt.format(new Date(viewYear, viewMonth, 1));
    calGrid.replaceChildren();

    const firstWeekday = new Date(viewYear, viewMonth, 1).getDay(); // 0 = Sun
    const leading = (firstWeekday - weekStart + 7) % 7;
    const daysInMonth = new Date(viewYear, viewMonth + 1, 0).getDate();
    const today = todayISO();

    let pendingButton: HTMLButtonElement | null = null;

    for (let i = 0; i < leading; i++) {
      const blank = document.createElement("div");
      blank.className = "h-9";
      blank.setAttribute("aria-hidden", "true");
      calGrid.appendChild(blank);
    }

    for (let d = 1; d <= daysInMonth; d++) {
      const iso = isoFromYMD(viewYear, viewMonth, d);
      const btn = document.createElement("button");
      btn.type = "button";
      btn.dataset.day = String(d);
      btn.dataset.iso = iso;
      btn.textContent = String(d);
      btn.setAttribute("aria-label", formatLongDate(iso));
      const isPending = iso === pending;
      const isToday = iso === today;
      if (isPending) btn.setAttribute("aria-pressed", "true");
      btn.tabIndex = isPending ? 0 : -1;
      const base =
        "h-9 rounded-md text-sm tabular-nums hover:bg-neutral-100 dark:hover:bg-neutral-800 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-neutral-400 dark:focus-visible:ring-neutral-500";
      const pendingCls =
        "bg-neutral-900 text-white hover:bg-neutral-700 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300";
      const todayCls =
        "ring-1 ring-inset ring-neutral-400 dark:ring-neutral-600";
      btn.className = [
        base,
        isPending ? pendingCls : "",
        isToday && !isPending ? todayCls : "",
      ]
        .filter(Boolean)
        .join(" ");
      btn.addEventListener("click", (e) => {
        e.preventDefault();
        pending = iso;
        // Re-render so the highlight follows the click. Stay open.
        const next = renderGrid();
        next?.focus();
      });
      if (isPending) pendingButton = btn;
      calGrid.appendChild(btn);
    }

    // Pad to a fixed 6-row × 7-column grid so the modal height doesn't jump.
    const totalCells = 6 * 7;
    const trailing = totalCells - leading - daysInMonth;
    for (let i = 0; i < trailing; i++) {
      const blank = document.createElement("div");
      blank.className = "h-9";
      blank.setAttribute("aria-hidden", "true");
      calGrid.appendChild(blank);
    }

    return pendingButton;
  };

  // Move pending by N days inside the grid. If we cross a month boundary,
  // reposition the visible month so the new day is on screen.
  const movePending = (deltaDays: number) => {
    const next = addDays(pending, deltaDays);
    pending = next;
    const p = parseISO(pending);
    if (p.y !== viewYear || p.m !== viewMonth) {
      viewYear = p.y;
      viewMonth = p.m;
    }
    const focusable = renderGrid();
    focusable?.focus();
  };

  // Initial trigger / cadence-badge render based on committed state.
  renderTrigger(hidden.value || todayISO());
  renderCadenceBadge(pendingCadence);

  openBtn.addEventListener("click", (e) => {
    e.preventDefault();
    // Reset pending state to whatever's currently committed.
    pending = hidden.value || todayISO();
    pendingCadence = cadenceHidden?.value ?? "";
    if (cadenceSelect) cadenceSelect.value = pendingCadence;
    const p = parseISO(pending);
    viewYear = p.y;
    viewMonth = p.m;
    renderWeekdays();
    const focusable = renderGrid();
    dialog.showModal();
    // Defer focus until after showModal lays out, otherwise some browsers
    // place focus on the first <button> in the form.
    queueMicrotask(() => focusable?.focus());
  });

  todayBtn.addEventListener("click", (e) => {
    e.preventDefault();
    const t = todayISO();
    pending = t;
    const p = parseISO(t);
    viewYear = p.y;
    viewMonth = p.m;
    const focusable = renderGrid();
    focusable?.focus();
  });

  calPrev.addEventListener("click", (e) => {
    e.preventDefault();
    if (viewMonth === 0) {
      viewMonth = 11;
      viewYear--;
    } else {
      viewMonth--;
    }
    renderGrid();
  });

  calNext.addEventListener("click", (e) => {
    e.preventDefault();
    if (viewMonth === 11) {
      viewMonth = 0;
      viewYear++;
    } else {
      viewMonth++;
    }
    renderGrid();
  });

  // Cadence dropdown: track pending only; commit happens via Done.
  if (cadenceSelect) {
    cadenceSelect.addEventListener("change", () => {
      pendingCadence = cadenceSelect.value;
    });
  }

  // Keyboard navigation inside the grid (roving tabindex). Arrow keys move
  // pending; PageUp/PageDown shift months; Home/End jump to first/last of
  // month; Enter activates Done.
  calGrid.addEventListener("keydown", (e) => {
    const target = e.target;
    if (!(target instanceof HTMLButtonElement) || !target.dataset.iso) return;
    let handled = true;
    switch (e.key) {
      case "ArrowLeft":
        movePending(-1);
        break;
      case "ArrowRight":
        movePending(1);
        break;
      case "ArrowUp":
        movePending(-7);
        break;
      case "ArrowDown":
        movePending(7);
        break;
      case "PageUp":
        movePending(e.shiftKey ? -365 : -28);
        break;
      case "PageDown":
        movePending(e.shiftKey ? 365 : 28);
        break;
      case "Home": {
        const p = parseISO(pending);
        pending = isoFromYMD(p.y, p.m, 1);
        renderGrid()?.focus();
        break;
      }
      case "End": {
        const p = parseISO(pending);
        const last = new Date(p.y, p.m + 1, 0).getDate();
        pending = isoFromYMD(p.y, p.m, last);
        renderGrid()?.focus();
        break;
      }
      case "Enter":
        // Same as clicking Done.
        doneBtn.click();
        break;
      default:
        handled = false;
    }
    if (handled) e.preventDefault();
  });

  // Done: commit pending values, then close. Cancel / X: just close.
  doneBtn.addEventListener("click", (e) => {
    e.preventDefault();
    if (hidden.value !== pending) {
      hidden.value = pending;
      hidden.dispatchEvent(new Event("change", { bubbles: true }));
    }
    if (cadenceHidden && cadenceHidden.value !== pendingCadence) {
      cadenceHidden.value = pendingCadence;
      cadenceHidden.dispatchEvent(new Event("change", { bubbles: true }));
    }
    renderTrigger(hidden.value);
    renderCadenceBadge(cadenceHidden?.value ?? "");
    dialog.close();
  });
  cancelBtns.forEach((btn) =>
    btn.addEventListener("click", (e) => {
      e.preventDefault();
      dialog.close();
    }),
  );

  // No backdrop-close listener on purpose — pointer cancellation (WCAG 2.5.7);
  // Escape closes via the native <dialog> behaviour without committing anything.
}

document.querySelectorAll<HTMLElement>("[data-date-picker]").forEach(setupPicker);
