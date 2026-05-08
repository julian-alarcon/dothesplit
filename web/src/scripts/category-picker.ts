// Category picker. Opens a <dialog> listing every category as a tappable
// row; selecting one updates the hidden input[name="category_id"], the
// emoji shown on the trigger button, and closes the dialog.
//
// Keyboard-accessible by virtue of native <dialog> + <button> elements
// (Escape closes via the browser, Tab cycles focus inside the dialog).
// No backdrop-close on purpose - matches DatePicker / SplitEditor for
// pointer cancellation (WCAG 2.5.7).

function setupCategoryPicker(root: HTMLElement) {
  const dialog = root.querySelector<HTMLDialogElement>("[data-category-dialog]");
  const openBtn = root.querySelector<HTMLElement>("[data-category-open]");
  const triggerEmoji = root.querySelector<HTMLElement>("[data-category-emoji]");
  const hiddenInput = root.querySelector<HTMLInputElement>('input[name="category_id"]');
  const cancelBtns = root.querySelectorAll<HTMLButtonElement>("[data-category-cancel]");
  if (!dialog || !openBtn || !triggerEmoji || !hiddenInput) return;

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
      const emoji = btn.dataset.emoji ?? "";
      if (!id) return;
      hiddenInput.value = id;
      triggerEmoji.textContent = emoji;
      dialog.close();
    });
  });
}

document
  .querySelectorAll<HTMLElement>("[data-category-picker]")
  .forEach(setupCategoryPicker);

export {};
