import { expect, test } from "@playwright/test";

/**
 * Smoke E2E: drives the install ceremony + group create through the actual
 * SSR Astro pages, hitting the real Go API. The point is to catch contract
 * drift between web and api end-to-end - a green Go suite + green Astro
 * build can both pass while the SSR bridge sends fields the API doesn't
 * accept (or vice versa). One end-to-end test catches that class of bug.
 *
 * Stack assumptions:
 *   - WEB_BASE_URL points at a running web container (defaults to localhost:3000).
 *   - SETUP_TOKEN env var carries the install token. The CI job greps it from
 *     `docker compose logs api` after boot. Locally, copy it from the API
 *     container's startup log.
 *
 * The test deliberately avoids the dashboard's expense/split-editor flow:
 * those are heavy interactive widgets and belong in their own focused tests
 * (see "should have" tier in the test plan).
 */

const TOKEN = process.env.SETUP_TOKEN;
const ADMIN_EMAIL = "admin-e2e@test.dev";
const ADMIN_NAME = "Admin E2E";
const PASSWORD = "passwordpassword"; // 16 chars, satisfies the 10-char min.

test.describe.configure({ mode: "serial" });

test("first-run setup mints an admin", async ({ page }) => {
  test.skip(!TOKEN, "SETUP_TOKEN env var is required for E2E (see docker compose logs api)");

  await page.goto("/setup");
  // The setup page's heading is the welcome banner; the form is the only
  // input[name="token"] in the app, which is a tighter assertion than text.
  await expect(page.locator('input[name="token"]')).toBeVisible();

  await page.locator('input[name="token"]').fill(TOKEN!);
  await page.locator('input[name="display_name"]').fill(ADMIN_NAME);
  await page.locator('input[name="email"]').fill(ADMIN_EMAIL);
  await page.locator('input[name="password"]').fill(PASSWORD);
  await page.getByRole("button", { name: /create admin/i }).click();

  // Successful setup redirects to /groups (the post-login landing page).
  await expect(page).toHaveURL(/\/groups\/?$/);
});

test("admin can create a new group", async ({ page }) => {
  test.skip(!TOKEN, "SETUP_TOKEN env var required");

  // Continue from the first test's session. If running in isolation,
  // log in again - admins keep their cookies across tests in this file.
  await page.goto("/groups");
  if (page.url().includes("/login")) {
    await page.locator('input[name="email"]').fill(ADMIN_EMAIL);
    await page.locator('input[name="password"]').fill(PASSWORD);
    await page.getByRole("button", { name: /log in/i }).click();
    await expect(page).toHaveURL(/\/groups\/?$/);
  }

  await page.goto("/groups/new");
  await page.locator('input[name="name"]').fill("Smoke Trip");
  await page.getByRole("button", { name: /create group/i }).click();

  // Group dashboard URL is /groups/<uuid>; assert we landed there and the
  // name we just typed is rendered somewhere on the page.
  await expect(page).toHaveURL(/\/groups\/[0-9a-f-]+/i);
  await expect(page.getByText("Smoke Trip")).toBeVisible();
});
