import { chromium, request } from '@playwright/test';

const BASE = process.env.BULWARK_BASE_URL ?? 'http://localhost:3000';
const JMAP = process.env.JMAP_SERVER_URL ?? 'https://localhost:8443';

(async () => {
  const u = 'recipient-' + Date.now() + '@example.com';
  const ctx = await request.newContext({
    ignoreHTTPSErrors: true,
    baseURL: JMAP,
    extraHTTPHeaders: { Authorization: `Basic ${Buffer.from(`${u}:${u}`).toString('base64')}` },
  });
  const session = await (await ctx.get('/.well-known/jmap')).json();
  const acc = session.primaryAccounts['urn:ietf:params:jmap:mail'];
  const mailboxes = await (
    await ctx.post(session.apiUrl, { data: { using: ['urn:ietf:params:jmap:core', 'urn:ietf:params:jmap:mail'], methodCalls: [['Mailbox/get', { accountId: acc }, 'm']] } })
  ).json();
  const inbox = mailboxes.methodResponses[0][1].list.find((m) => m.role === 'inbox').id;
  const subject = 'Protocol body probe ' + Date.now();
  await ctx.post(session.apiUrl, {
    data: {
      using: ['urn:ietf:params:jmap:core', 'urn:ietf:params:jmap:mail'],
      methodCalls: [[
        'Email/set', {
          accountId: acc,
          create: {
            k1: {
              mailboxIds: { [inbox]: true },
              subject,
              from: [{ name: 'Sender', email: 's@example.com' }],
              to: [{ email: u }],
              keywords: { $seen: true },
              bodyValues: { '1': { value: 'Created via JMAP, viewed via Bulwark' } },
              textBody: [{ partId: '1', type: 'text/plain' }],
            },
          },
        }, 'c1'],
      ],
    },
  });

  const browser = await chromium.launch({ args: ['--ignore-certificate-errors'] });
  const page = await (await browser.newContext({ ignoreHTTPSErrors: true })).newPage();
  await page.goto(BASE, { waitUntil: 'domcontentloaded' });
  const urlField = page.locator('input[type="url"]');
  await urlField.waitFor({ timeout: 30000 });
  if (await urlField.isEditable().catch(() => false)) await urlField.fill(JMAP);
  await page.fill('input[type="text"]', u);
  await page.fill('input[type="password"]', u);
  await page.click('button[type="submit"]');
  await page.waitForURL(/\/en(\/|$)/, { timeout: 60000 });
  const gotIt = page.getByRole('button', { name: 'Got it' });
  if (await gotIt.isVisible({ timeout: 3000 }).catch(() => false)) await gotIt.click();
  await page.waitForTimeout(2500);

  console.log('subject visible:', await page.getByText(subject).first().isVisible().catch(() => false));
  const items = page.locator('[data-testid="email-list-item"]');
  console.log('items:', await items.count());
  const itemTexts = [];
  for (let i = 0; i < await items.count(); i++) itemTexts.push(((await items.nth(i).innerText().catch(() => '')) || '').slice(0, 80));
  console.log('item texts:', JSON.stringify(itemTexts));

  await items.first().click();
  await page.waitForTimeout(2500);
  console.log('reply visible:', await page.getByRole('button', { name: 'Reply', exact: true }).first().isVisible().catch(() => false));
  const bodyText = await page.evaluate(() => document.body.innerText.slice(0, 2000));
  console.log('BODY TEXT:', JSON.stringify(bodyText));
  const html = await page.evaluate(() => document.body.innerHTML);
  const m = html.match(/Created via JMAP[^<]{0,120}/g);
  console.log('created-via matches:', JSON.stringify(m));
  await page.screenshot({ path: '/tmp/opencode/protocol-body.png' });
  await browser.close();
})();