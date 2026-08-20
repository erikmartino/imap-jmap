import { test, expect } from '@playwright/test';
import { login, uniqueUser, BASE_URL, JMAP_URL } from '../lib/helpers';

test.describe('login', () => {
  test('signs in a valid account and lands on the mailbox', async ({ page }) => {
    const user = uniqueUser('login-user');
    await login(page, user.username, user.password);
    // The exact post-login route varies by Bulwark version (older builds land on
    // /mail/folder/inbox, newer ones on the locale home /en), so assert the mailbox
    // UI itself: the Inbox folder row with the seeded unread count is present on
    // every logged-in mail view.
    await expect(page.getByRole('button', { name: /^Inbox/ }).first()).toBeVisible({ timeout: 15_000 });
  });

  test('rejects invalid credentials with an inline error', async ({ page }) => {
    await page.goto(BASE_URL, { waitUntil: 'domcontentloaded' });
    const urlField = page.locator('input[type="url"]');
    if (await urlField.isVisible({ timeout: 2000 }).catch(() => false)) {
      if (await urlField.isEditable().catch(() => false)) {
        await urlField.fill(JMAP_URL);
      }
    }
    await page.fill('input[type="text"], input[type="email"]', 'invalid@example.com');
    await page.fill('input[type="password"]', 'wrong-password');
    await page.click('button[type="submit"]');
    await expect(page.getByText(/invalid email or password/i)).toBeVisible({ timeout: 15_000 });
    await expect(page).toHaveURL(/\/login/);
  });

  test('logs out back to the login page', async ({ page }) => {
    const user = uniqueUser('logout-user');
    await login(page, user.username, user.password);
    await page.locator('[data-testid="account-switcher"]').first().click();
    const logout = page.getByRole('menuitem', { name: /sign out/i }).first();
    if (await logout.isVisible({ timeout: 3000 }).catch(() => false)) {
      await logout.click();
    }
    await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible({ timeout: 15_000 });
  });
});