import { test, expect } from '@playwright/test';
import { login, uniqueUser, JMAPClient, goToApp } from '../lib/helpers';

test.describe('calendar & contacts', () => {
  test('calendar app shows the seeded personal calendar', async ({ page }) => {
    const user = uniqueUser('pim-cal-user');
    await login(page, user.username, user.password);
    await goToApp(page, '/en/calendar');

    await expect(page.getByText('Personal Calendar').first()).toBeVisible({ timeout: 15_000 });
    await expect(page.getByRole('button', { name: 'Create event' }).first()).toBeVisible();
    for (const tab of ['Month', 'Week', 'Day', 'Agenda']) {
      await expect(page.getByRole('button', { name: tab, exact: true }).first()).toBeVisible();
    }
  });

  test('creates a contact from the UI and reads it back over the protocol', async ({ page }) => {
    const acct = uniqueUser('contact');
    const jmap = await JMAPClient.connect(acct.username, acct.password);
    const firstName = `E2E${Date.now() % 1000000}`;
    const lastName = 'Tester';
    const email = `${firstName.toLowerCase()}@example.com`;

    await login(page, acct.username, acct.password);
    await goToApp(page, '/en/contacts');

    await page.locator('button:has(svg.lucide-plus.w-4)').first().click();
    await page.getByRole('button', { name: 'New Contact' }).first().click();
    await expect(page.getByPlaceholder('First name')).toBeVisible({ timeout: 15_000 });
    await page.getByPlaceholder('First name').fill(firstName);
    await page.getByPlaceholder('Last name').fill(lastName);
    await page.getByPlaceholder('email@example.com').fill(email);
    await page.getByRole('button', { name: 'Save', exact: true }).click();

    // The contact shows up in the address book list.
    await expect(page.getByText(firstName).first()).toBeVisible({ timeout: 20_000 });

    // And it is persisted server-side per RFC 9610 (ContactCard/set → query/get).
    await expect
      .poll(async () => (await jmap.contactsByEmail(email)).length, { timeout: 20_000 })
      .toBeGreaterThan(0);
  });
});