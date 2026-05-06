// Date picker: opens a <dialog> with a custom month-grid calendar and writes
// the picked YYYY-MM-DD into a hidden input the parent form posts. The trigger
// button shows a calendar SVG with the picked day rendered over it, so the
// icon itself doubles as a visual current-date indicator.

const labelFmt = new Intl.DateTimeFormat(undefined, {
  weekday: "short",
  year: "numeric",
  month: "short",
  day: "numeric",
});
const monthTitleFmt = new Intl.DateTimeFormat(undefined, {
  month: "long",
  year: "numeric",
});
// First locale weekday is Sunday by default; for users in regions where
// Monday-first is more natural this would need a locale lookup. Keep it
// simple for v1 — match what `<input type="date">` was already doing.
const weekdayFmt = new Intl.DateTimeFormat(undefined, { weekday: "short" });

function formatLabel(yyyymmdd: string): string {
  // Anchor at noon UTC to match what the SSR handler does on submit, so the
  // displayed weekday matches what the server will store.
  const d = new Date(`${yyyymmdd}T12:00:00Z`);
  return Number.isNaN(d.getTime()) ? yyyymmdd : labelFmt.format(d);
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

function setupPicker(root: HTMLElement) {
  const dialog = root.querySelector<HTMLDialogElement>("[data-date-dialog]");
  const openBtn = root.querySelector<HTMLButtonElement>("[data-date-open]");
  const hidden = root.querySelector<HTMLInputElement>("[data-date-value]");
  const label = root.querySelector<HTMLElement>("[data-date-label]");
  const dayBadge = root.querySelector<HTMLElement>("[data-date-day]");
  const todayBtn = root.querySelector<HTMLButtonElement>("[data-date-today]");
  const cancelBtns = root.querySelectorAll<HTMLButtonElement>("[data-date-cancel]");
  const calTitle = root.querySelector<HTMLElement>("[data-cal-title]");
  const calPrev = root.querySelector<HTMLButtonElement>("[data-cal-prev]");
  const calNext = root.querySelector<HTMLButtonElement>("[data-cal-next]");
  const calWeekdays = root.querySelector<HTMLElement>("[data-cal-weekdays]");
  const calGrid = root.querySelector<HTMLElement>("[data-cal-grid]");
  // `label` is optional — the compact variant omits it.
  if (
    !dialog || !openBtn || !hidden || !dayBadge || !todayBtn ||
    !calTitle || !calPrev || !calNext || !calWeekdays || !calGrid
  ) return;

  // viewYear / viewMonth = currently visible month grid.
  // selected = the committed date (mirrors hidden.value).
  const initial = parseISO(hidden.value || todayISO());
  let viewYear = initial.y;
  let viewMonth = initial.m;

  const labelEl = label;
  const renderTrigger = (iso: string) => {
    if (labelEl) {
      labelEl.textContent = formatLabel(iso);
    }
    const day = Number(iso.slice(8, 10));
    if (Number.isFinite(day) && day > 0) dayBadge.textContent = String(day);
  };

  // Programmatic writes to a hidden input don't fire input/change events.
  // Emit one explicitly so listeners (dirty-form, etc.) react to picks.
  const commit = (iso: string) => {
    if (hidden.value === iso) return;
    hidden.value = iso;
    hidden.dispatchEvent(new Event("change", { bubbles: true }));
  };

  const renderWeekdays = () => {
    // Fill once. Use a known Sunday (2024-01-07) as the anchor.
    if (calWeekdays.childElementCount > 0) return;
    const anchor = new Date(Date.UTC(2024, 0, 7));
    for (let i = 0; i < 7; i++) {
      const d = new Date(anchor.getTime() + i * 24 * 60 * 60 * 1000);
      const cell = document.createElement("div");
      cell.textContent = weekdayFmt.format(d).slice(0, 2);
      calWeekdays.appendChild(cell);
    }
  };

  const renderGrid = () => {
    calTitle.textContent = monthTitleFmt.format(new Date(viewYear, viewMonth, 1));
    calGrid.replaceChildren();

    const firstWeekday = new Date(viewYear, viewMonth, 1).getDay(); // 0 = Sun
    const daysInMonth = new Date(viewYear, viewMonth + 1, 0).getDate();
    const today = todayISO();
    const selected = hidden.value;

    // Leading blanks for alignment.
    for (let i = 0; i < firstWeekday; i++) {
      const blank = document.createElement("div");
      blank.className = "h-9";
      calGrid.appendChild(blank);
    }

    for (let d = 1; d <= daysInMonth; d++) {
      const iso = isoFromYMD(viewYear, viewMonth, d);
      const btn = document.createElement("button");
      btn.type = "button";
      btn.dataset.day = String(d);
      btn.textContent = String(d);
      const isSelected = iso === selected;
      const isToday = iso === today;
      const base =
        "h-9 rounded-md text-sm tabular-nums hover:bg-neutral-100 dark:hover:bg-neutral-800";
      const selectedCls =
        "bg-neutral-900 text-white hover:bg-neutral-700 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300";
      const todayCls =
        "ring-1 ring-inset ring-neutral-400 dark:ring-neutral-600";
      btn.className = [
        base,
        isSelected ? selectedCls : "",
        isToday && !isSelected ? todayCls : "",
      ]
        .filter(Boolean)
        .join(" ");
      btn.addEventListener("click", (e) => {
        e.preventDefault();
        commit(iso);
        renderTrigger(iso);
        // Re-render grid so the highlight follows the click, then close.
        renderGrid();
        dialog.close();
      });
      calGrid.appendChild(btn);
    }

    // Pad to a fixed 6-row × 7-column grid so the modal height doesn't jump
    // between months. (5-week months would otherwise be 36px shorter.)
    const totalCells = 6 * 7;
    const trailing = totalCells - firstWeekday - daysInMonth;
    for (let i = 0; i < trailing; i++) {
      const blank = document.createElement("div");
      blank.className = "h-9";
      calGrid.appendChild(blank);
    }
  };

  // Wire-up.
  renderTrigger(hidden.value || todayISO());
  renderWeekdays();

  openBtn.addEventListener("click", (e) => {
    e.preventDefault();
    // Open the grid on the selected month so the user lands on what they last picked.
    const v = parseISO(hidden.value || todayISO());
    viewYear = v.y;
    viewMonth = v.m;
    renderGrid();
    dialog.showModal();
  });

  cancelBtns.forEach((btn) =>
    btn.addEventListener("click", (e) => {
      e.preventDefault();
      dialog.close();
    }),
  );

  todayBtn.addEventListener("click", (e) => {
    e.preventDefault();
    const iso = todayISO();
    const t = parseISO(iso);
    viewYear = t.y;
    viewMonth = t.m;
    commit(iso);
    renderTrigger(iso);
    renderGrid();
    dialog.close();
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

  // Backdrop click closes.
  dialog.addEventListener("click", (e) => {
    const rect = dialog.getBoundingClientRect();
    const inside =
      e.clientX >= rect.left && e.clientX <= rect.right && e.clientY >= rect.top && e.clientY <= rect.bottom;
    if (!inside) dialog.close();
  });
}

document.querySelectorAll<HTMLElement>("[data-date-picker]").forEach(setupPicker);
