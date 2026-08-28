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

  test('edits the seeded Alice Smith contact, asserts pre-filled name fields and saves', async ({ page }) => {
    const acct = uniqueUser('alice-edit');
    const jmap = await JMAPClient.connect(acct.username, acct.password);

    await login(page, acct.username, acct.password);
    await goToApp(page, '/en/contacts');

    // Click seeded Alice Smith contact
    const aliceContact = page.getByText('Alice Smith').first();
    await expect(aliceContact).toBeVisible({ timeout: 20_000 });
    await aliceContact.click();

    // Click Edit
    const editBtn = page.getByRole('button', { name: 'Edit contact' }).or(page.getByRole('button', { name: 'Edit' })).first();
    await expect(editBtn).toBeVisible({ timeout: 15_000 });
    await editBtn.click();

    // Assert First name and Last name are populated (not empty)
    const firstNameInput = page.getByPlaceholder('First name');
    const lastNameInput = page.getByPlaceholder('Last name');
    await expect(firstNameInput).toBeVisible({ timeout: 15_000 });
    await expect(firstNameInput).toHaveValue('Alice');
    await expect(lastNameInput).toHaveValue('Smith');

    // Update last name
    const updatedLastName = 'Smith-Updated';
    await lastNameInput.fill(updatedLastName);
    await page.getByRole('button', { name: 'Save', exact: true }).first().click();

    await expect(page.getByText('Contact updated').first()).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText('Failed to update')).toBeHidden();

    // Verify over UI
    await expect(page.getByText(`Alice ${updatedLastName}`).first()).toBeVisible({ timeout: 20_000 });

    // Verify over JMAP API
    await expect
      .poll(async () => {
        const cards = await jmap.contactsByEmail('alice@example.com');
        return cards[0]?.name?.full || cards[0]?.name?.components?.find((c: any) => c.kind === 'surname')?.value;
      }, { timeout: 20_000 })
      .toContain(updatedLastName);
  });
});