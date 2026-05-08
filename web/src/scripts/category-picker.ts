// Category picker. Opens a <dialog> listing every category as a tappable
// row; selecting one updates the hidden input[name="category_id"], the
// SVG icon shown on the trigger button, and closes the dialog.
//
// The trigger and each option both render a CategoryIcon (an astro-icon
// <svg>). On selection we clone the chosen option's <svg> into the trigger
// container so the bundled icon + per-group color class transfer over
// without re-rendering anything.
//
// Keyboard-accessible by virtue of native <dialog> + <button> elements
// (Escape closes via the browser, Tab cycles focus inside the dialog).
// No backdrop-close on purpose - matches DatePicker / SplitEditor for
// pointer cancellation (WCAG 2.5.7).

function setupCategoryPicker(root: HTMLElement) {
  const dialog = root.querySelector<HTMLDialogElement>("[data-category-dialog]");
  const openBtn = root.querySelector<HTMLElement>("[data-category-open]");
  const triggerIcon = root.querySelector<HTMLElement>("[data-category-icon]");
  const hiddenInput = root.querySelector<HTMLInputElement>('input[name="category_id"]');
  const cancelBtns = root.querySelectorAll<HTMLButtonElement>("[data-category-cancel]");
  if (!dialog || !openBtn || !triggerIcon || !hiddenInput) return;

  openBtn.addEventListener("click", (e) => {
    e.preventDefault();
    dialog.showModal();
  });

  cancelBtns.forEach((btn) => {
    btn.addEventListener("click", (e) => {
      e.preventDefault();
      dialog.close();
    });
  });

  root.querySelectorAll<HTMLButtonElement>("[data-category-option]").forEach((btn) => {
    btn.addEventListener("click", (e) => {
      e.preventDefault();
      const id = btn.dataset.categoryId ?? "";
      if (!id) return;
      hiddenInput.value = id;
      // Programmatic value changes don't fire input/change events by default,
      // so the dirty-form watcher (and any other listener) wouldn't notice.
      hiddenInput.dispatchEvent(new Event("input", { bubbles: true }));
      hiddenInput.dispatchEvent(new Event("change", { bubbles: true }));
      const sourceSvg = btn.querySelector("svg");
      if (sourceSvg) {
        triggerIcon.replaceChildren(sourceSvg.cloneNode(true));
      }
      dialog.close();
    });
  });
}

document
  .querySelectorAll<HTMLElement>("[data-category-picker]")
  .forEach(setupCategoryPicker);

export {};
