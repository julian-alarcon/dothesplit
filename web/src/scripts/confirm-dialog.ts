// Confirmation dialog wiring.
//
// Trigger buttons on a page set `data-confirm-open` to the id of a
// <ConfirmDialog id="…"> on the same page. The trigger MUST live inside the
// <form> that should be submitted on confirm — we walk up to it via .closest().
//
// Backdrop click intentionally does not close (WCAG 2.5.7 pointer
// cancellation, consistent with the DatePicker dialog). Escape closes via
// the native <dialog> behaviour with no commit.

function setup() {
  const triggers = document.querySelectorAll<HTMLElement>("[data-confirm-open]");
  for (const trigger of triggers) {
    const dialogId = trigger.dataset.confirmOpen;
    if (!dialogId) continue;
    const dialog = document.getElementById(dialogId) as HTMLDialogElement | null;
    if (!dialog) continue;

    // Each dialog is shared by potentially multiple triggers on a page (e.g.
    // one "Remove" button per member). Track which form to submit on accept.
    trigger.addEventListener("click", (e) => {
      e.preventDefault();
      const form = trigger.closest("form");
      if (!form) return;
      // Stash on the dialog itself so accept handler reads the right form even
      // if a different trigger was clicked since.
      (dialog as HTMLDialogElement & { _targetForm?: HTMLFormElement })._targetForm = form;
      dialog.showModal();
      // Defer focus so showModal() lays out first; focus the cancel button as
      // a safer default than auto-focusing the destructive action.
      queueMicrotask(() => {
        const cancel = dialog.querySelector<HTMLButtonElement>("[data-confirm-cancel]");
        cancel?.focus();
      });
    });
  }

  const dialogs = document.querySelectorAll<HTMLDialogElement>("[data-confirm-dialog]");
  for (const dialog of dialogs) {
    const cancels = dialog.querySelectorAll<HTMLButtonElement>("[data-confirm-cancel]");
    for (const btn of cancels) {
      btn.addEventListener("click", (e) => {
        e.preventDefault();
        dialog.close();
      });
    }

    const accept = dialog.querySelector<HTMLButtonElement>("[data-confirm-accept]");
    accept?.addEventListener("click", (e) => {
      e.preventDefault();
      const form = (dialog as HTMLDialogElement & { _targetForm?: HTMLFormElement })._targetForm;
      dialog.close();
      // requestSubmit() runs the form's onsubmit listeners and submits it.
      // submit() bypasses validation; we want validation.
      form?.requestSubmit();
    });
  }
}

setup();
