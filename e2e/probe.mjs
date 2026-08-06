import { chromium } from '@playwright/test';

const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ ignoreHTTPSErrors: true });
const page = await context.newPage();
page.on('console', (m) => { if (m.type() === 'error') console.log('CONSOLE-ERR', m.text().slice(0, 200)); });
page.on('requestfailed', (r) => console.log('REQFAIL', r.url().slice(0, 120)));

async function login(user) {
  await page.goto('http://localhost:3000', { waitUntil: 'domcontentloaded' });
  await page.fill('input[type="url"]', 'https://localhost:8443');
  await page.fill('input[type="text"]', user);
  await page.fill('input[type="password"]', user);
  await page.click('button[type="submit"]');
  await page.waitForURL(/\/en(\/|$)/, { timeout: 30000 });
  await page.waitForTimeout(1200);
  const gotit = page.getByRole('button', { name: 'Got it' });
  if (await gotit.isVisible().catch(() => false)) await gotit.click();
}

const fresh = `probe-${Date.now()}@example.com`;
await login(fresh);

// 1. compose save status tracking
await page.keyboard.press('c');
await page.waitForSelector('[data-testid="email-composer"]');
await page.locator('[data-testid="composer-to"] input').fill(`to-${Date.now()}@example.com`);
await page.locator('input[data-testid="composer-subject"]').fill('Probe subject');
await page.locator('[data-testid="email-composer"] .ProseMirror').click();
await page.keyboard.type('probe body');
const seenStatuses = new Set();
for (let i = 0; i < 20; i++) {
  const s = await page.locator('[data-testid="composer-save-status"]').getAttribute('data-status').catch(() => null);
  if (s) seenStatuses.add(s);
  await page.waitForTimeout(500);
}
console.log('composer-save-status values seen:', [...seenStatuses]);
const stText = await page.locator('[data-testid="composer-save-status"]').innerText({ timeout: 3000 }).catch(() => 'N/A');
console.log('status text:', JSON.stringify(stText));

// 2. account switcher / logout menu
await page.locator('[data-testid="account-switcher"]').first().click();
await page.waitForTimeout(700);
const menuItems = await page.locator('[role="menuitem"], [role="menu"] *').evaluateAll((els) => els.map((e) => (e.textContent || '').trim().slice(0, 40)).filter(Boolean).slice(0, 20));
console.log('switcher menu items:', JSON.stringify(menuItems));
await page.keyboard.press('Escape').catch(() => {});
await page.waitForTimeout(300);

// 3. calendar
await page.locator('a[href="/en/calendar"]').first().click();
await page.waitForURL('**/en/calendar');
await page.waitForTimeout(2500);
console.log('calendar contains Personal Calendar:', await page.getByText('Personal Calendar').count());
console.log('calendar create event:', await page.getByRole('button', { name: 'Create event' }).count());
const calText = (await page.locator('body').innerText());
console.log('calendar mention of event:', /Q3 Product Architecture/.test(calText), '/Team Lunch:', /Team Lunch/.test(calText), '/Welcome & Onboarding:', /Welcome & Onboarding/.test(calText));

// 4. contacts list display format after save
await page.locator('a[href="/en/contacts"]').first().click();
await page.waitForURL('**/en/contacts');
await page.waitForTimeout(1500);
await page.getByRole('button', { name: 'New Contact' }).first().click();
await page.waitForTimeout(800);
const fn = 'Probe' + (Date.now() % 100000);
await page.getByPlaceholder('First name').fill(fn);
await page.getByPlaceholder('Last name').fill('Doe');
await page.getByPlaceholder('email@example.com').fill(fn.toLowerCase() + '@example.com');
await page.getByRole('button', { name: 'Save', exact: true }).click();
await page.waitForTimeout(2000);
console.log('contact list contains first name:', await page.getByText(fn).count());
console.log('contact list body snippet:', (await page.locator('body').innerText()).slice(0, 500).replace(/\n+/g, ' | '));

// 5. archive: create msg via API then archive
const { request } = await import('@playwright/test');
const ctx = await request.newContext({ ignoreHTTPSErrors: true, baseURL: 'https://localhost:8443', extraHTTPHeaders: { Authorization: `Basic ${Buffer.from(fresh + ':' + fresh).toString('base64')}` } });
const sess = await (await ctx.get('/.well-known/jmap')).json();
const acc = sess.primaryAccounts['urn:ietf:params:jmap:mail'];
const mb = await (await ctx.post(sess.apiUrl, { data: { using: ['urn:ietf:params:jmap:core', 'urn:ietf:params:jmap:mail'], methodCalls: [['Mailbox/get', { accountId: acc }, 'm']] } })).json();
const inboxId = mb.methodResponses[0][1].list.find((x) => x.role === 'inbox').id;
await ctx.post(sess.apiUrl, { data: { using: ['urn:ietf:params:jmap:core', 'urn:ietf:params:jmap:mail'], methodCalls: [['Email/set', { accountId: acc, create: { k1: { mailboxIds: { [inboxId]: true }, subject: 'Archive probe', bodyValues: { '1': { value: 'body' } }, textBody: [{ partId: '1', type: 'text/plain' }] } } }, 'set']] } });
// navigate to mail via SPA
await page.locator('a[href="/en"]').first().click();
await page.waitForURL('**/en');
await page.waitForTimeout(2000);
console.log('archive probe in list:', await page.getByText('Archive probe').count());
await page.locator('[data-testid="email-list-item"]', { hasText: 'Archive probe' }).first().click();
await page.waitForTimeout(1200);
const archiveBtn = page.getByRole('button', { name: 'Archive', exact: true });
console.log('archive button visible:', await archiveBtn.count());
await archiveBtn.first().click();
await page.waitForTimeout(3000);
const q2 = await (await ctx.post(sess.apiUrl, { data: { using: ['urn:ietf:params:jmap:core', 'urn:ietf:params:jmap:mail'], methodCalls: [['Email/query', { accountId: acc, filter: { subject: 'Archive probe' } }, 'q'], ['Email/get', { accountId: acc, ids: [], properties: ['mailboxIds'] }, 'g']] } })).json();
console.log('archive query resp:', JSON.stringify(q2.methodResponses).slice(0, 300));
await browser.close();