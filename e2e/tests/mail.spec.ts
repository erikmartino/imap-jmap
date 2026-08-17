import { test, expect } from '@playwright/test';
import { login, uniqueUser, JMAPClient, openComposer } from '../lib/helpers';

test.describe('mail', () => {
  test('renders the seeded inbox with sample messages', async ({ page }) => {
    const user = uniqueUser('inbox-user');
    await login(page, user.username, user.password);
    await expect(page.getByText('Welcome to JMAP Server').first()).toBeVisible({ timeout: 15_000 });
    await expect(page.getByText('JMAP Core and Mail Specifications').first()).toBeVisible();
    await expect(page.locator('[data-testid="email-list-item"]').first()).toBeVisible();
  });

  test('opens a message in the reading view', async ({ page }) => {
    const user = uniqueUser('reader-user');
    await login(page, user.username, user.password);
    await page
      .locator('[data-testid="email-list-item"]', { hasText: 'JMAP Core and Mail Specifications' })
      .first()
      .click();
    await expect(page.getByRole('button', { name: 'Reply', exact: true }).first()).toBeVisible({ timeout: 15_000 });
    await expect(page.getByText('This email verifies that your server supports RFC 8620').last()).toBeVisible();
  });

  test('composes, auto-saves a draft and sends to a local recipient', async ({ page }) => {
    const sender = uniqueUser('sender');
    const recipient = uniqueUser('recipient');
    const subject = `E2E composed message ${Date.now()}`;

    const senderJmap = await JMAPClient.connect(sender.username, sender.password);
    const recipientJmap = await JMAPClient.connect(recipient.username, recipient.password);

    await login(page, sender.username, sender.password);
    await openComposer(page);

    await page.locator('[data-testid="composer-to"] input').fill(recipient.username);
    await page.locator('[data-testid="composer-to"] input').press('Tab');
    await page.locator('input[data-testid="composer-subject"]').fill(subject);
    await page.locator('[data-testid="email-composer"] .ProseMirror').click();
    await page.keyboard.type('Hello from the Bulwark e2e suite!');

    // The server MUST accept the Bulwark draft shape (body part referenced by
    // partId without an explicit "type", RFC 8621 Section 4.2.2) and report a
    // successful auto-save.
    await expect(
      page.locator('[data-testid="composer-save-status"][data-status="saved"]'),
    ).toBeVisible({ timeout: 20_000 });

    // The draft must be stored in the Drafts mailbox over the wire protocol.
    await senderJmap.waitForEmailInRole(subject, 'drafts', 15_000);

    await page.locator('[data-testid="composer-send"]').last().click();
    await expect(page.locator('[data-testid="email-composer"]')).toBeHidden({ timeout: 30_000 });

    // Sent copy landed in the sender's Sent mailbox (RFC 8621 Section 7.5).
    await senderJmap.waitForEmailInRole(subject, 'sent', 30_000);

    // Local delivery put a copy in the recipient's Inbox (RFC 8621 Section 4.1).
    await recipientJmap.waitForEmailInRole(subject, 'inbox', 30_000);
  });

  test('a message created over the protocol renders in the recipient UI', async ({ page }) => {
    const recipient = uniqueUser('recipient');
    const jmap = await JMAPClient.connect(recipient.username, recipient.password);
    const subject = `E2E protocol-created message ${Date.now()}`;

    const inboxId = await jmap.mailboxIdByRole('inbox');
    const create = await jmap.api([
      ['Email/set', {
        create: {
          k1: {
            mailboxIds: { [inboxId!]: true },
            subject,
            from: [{ name: 'Protocol Sender', email: 'protocol-sender@example.com' }],
            to: [{ email: recipient.username }],
            keywords: { $seen: true },
            bodyValues: { '1': { value: 'Created via JMAP, viewed via Bulwark' } },
            textBody: [{ partId: '1', type: 'text/plain' }],
          },
        },
      }, 'c1'],
    ]);
    expect(create[0][1].created?.k1?.id).toBeTruthy();

    await login(page, recipient.username, recipient.password);
    await expect(page.getByText(subject).first()).toBeVisible({ timeout: 15_000 });

    await page.locator('[data-testid="email-list-item"]').first().click();
    await expect(page.getByRole('button', { name: 'Reply', exact: true }).first()).toBeVisible({ timeout: 15_000 });
    await expect(page.getByText('Created via JMAP, viewed via Bulwark').last()).toBeVisible();
  });

  test('archiving a message removes it from the inbox', async ({ page }) => {
    const acct = uniqueUser('archive');
    const jmap = await JMAPClient.connect(acct.username, acct.password);
    const subject = `E2E archive me ${Date.now()}`;

    const inboxId = await jmap.mailboxIdByRole('inbox');
    const create = await jmap.api([
      ['Email/set', {
        create: {
          k1: {
            mailboxIds: { [inboxId!]: true },
            subject,
            from: { name: 'Archive Sender', email: 'archive-sender@example.com' },
            to: [{ email: acct.username }],
            keywords: { $seen: true },
            bodyValues: { '1': { value: 'Please archive me' } },
            textBody: [{ partId: '1', type: 'text/plain' }],
          },
        },
      }, 'c1'],
    ]);
    const emailId = create[0][1].created.k1.id as string;

    await login(page, acct.username, acct.password);
    await expect(page.getByText(subject).first()).toBeVisible({ timeout: 15_000 });
    await page.locator('[data-testid="email-list-item"]').first().click();
    await page.getByRole('button', { name: 'Archive', exact: true }).last().click({ timeout: 15_000 });

    async function inInbox(): Promise<boolean> {
      const emailIds = await jmap.emailsByIds([emailId]);
      return emailIds[0]?.mailboxIds?.[inboxId!] === true;
    }
    await expect.poll(async () => inInbox(), { timeout: 15_000 }).toBe(false);

    // Re-query the inbox through the SPA sidebar and confirm the message is gone.
    await page.locator('[data-testid="folder-row"]', { hasText: 'Inbox' }).first().click();
    await expect(page.locator('[data-testid="email-list-item"]', { hasText: subject })).toBeHidden({ timeout: 10_000 });
  });
});