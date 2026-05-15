// Activity feed Load-more island. Fetches the next page of activity items as
// an HTML fragment from /groups/{id}/activity-fragment?cursor=... and appends
// the rendered <li> rows into the existing list. Updates the button's
// data-cursor in place; hides it when the server returns no next_cursor.

const button = document.querySelector<HTMLButtonElement>("[data-activity-load-more]");
const list = document.querySelector<HTMLUListElement>("[data-activity-list]");

if (button && list) {
  const groupID = button.dataset.groupId ?? "";

  button.addEventListener("click", async () => {
    if (button.disabled) return;
    const cursor = button.dataset.cursor ?? "";
    if (!cursor) return;
    button.disabled = true;
    const original = button.textContent;
    button.textContent = "Loading…";
    try {
      const url = new URL(`/groups/${groupID}/activity-fragment`, window.location.origin);
      url.searchParams.set("cursor", cursor);
      url.searchParams.set("limit", "25");
      const res = await fetch(url, { headers: { Accept: "text/html" } });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const html = await res.text();
      // Parse the returned fragment in a detached <template> so unrelated
      // page state isn't disturbed. The endpoint emits <li>... items plus a
      // hidden <div data-activity-next-cursor=...> trailer.
      const tpl = document.createElement("template");
      tpl.innerHTML = html;
      const trailer = tpl.content.querySelector<HTMLElement>("[data-activity-next-cursor]");
      const nextCursor = trailer?.dataset.activityNextCursor ?? "";
      trailer?.remove();
      // Drop the fragment's leading month header if it duplicates the last
      // header already on the page - happens when the next page starts
      // inside the same calendar month as the previous one.
      const existingHeaders = list.querySelectorAll<HTMLElement>("[data-month-header]");
      const lastKey = existingHeaders[existingHeaders.length - 1]?.dataset.monthKey;
      const firstFragmentHeader = tpl.content.querySelector<HTMLElement>("[data-month-header]");
      if (lastKey && firstFragmentHeader?.dataset.monthKey === lastKey) {
        firstFragmentHeader.remove();
      }
      list.append(tpl.content);
      if (nextCursor) {
        button.dataset.cursor = nextCursor;
        button.textContent = original;
        button.disabled = false;
      } else {
        button.remove();
      }
    } catch (err) {
      console.error("activity load-more failed", err);
      button.textContent = "Try again";
      button.disabled = false;
    }
  });
}

export {};
