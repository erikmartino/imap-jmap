import { chromium } from '@playwright/test';

const base = 'http://localhost:3000';
const user = `sender-${Date.now()}@example.com`;
const rcpt = `recipient-${Date.now()}@example.com`;

const browser = await chromium.launch({ headless: true });
const page = await (await browser.newContext({ ignoreHTTPSErrors: true })).newPage();

page.on('console', (msg) => {
  const t = msg.type();
  if (t === 'error' || t === 'warning') console.log('CONSOLE', t, ':', JSON.stringify(msg.text().slice(0, 300)));
});
page.on('pageerror', (err) => console.log('PAGEERROR:', JSON.stringify(String(err).slice(0, 300))));

let seq = 0;
page.on('request', (req) => {
  if (!req.url().includes('/jmap') || req.method() !== 'POST') return;
  seq++;
  const d = req.postData() || '';
  const j = JSON.parse(d);
  const names = j.methodCalls.map((c) => c[0]).join(',');
  if (names.includes('EmailSubmission') || names.includes('Email/set')) {
    console.log(`REQ#${seq}`, names, '|', d.slice(0, 700));
  }
});
page.on('response', async (resp) => {
  if (!resp.url().includes('/jmap') || resp.request().method() !== 'POST') return;
  const ct = resp.headers()['content-type'] || '';
  if (!ct.includes('json')) return;
  let json; try { json = JSON.parse(await resp.text()); } catch { return; }
  for (const [name, args] of json.methodResponses || []) {
    if (name === 'EmailSubmission/set' || name === 'Email/set') {
      const flags = [];
      for (const k of ['created', 'updated', 'destroyed', 'notCreated', 'notUpdated', 'notDestroyed']) {
        if (args?.[k] != null && (Array.isArray(args[k]) ? args[k].length : typeof args[k] === 'object' ? Object.keys(args[k]).length : true)) flags.push(k + '=' + JSON.stringify(args[k]).slice(0, 300));
      }
      console.log(`RESP#`, name, flags.join(' | '));
    }
    if (name === 'error') console.log('RESP-ERROR', JSON.stringify(args));
  }
});

await page.goto(base, { waitUntil: 'networkidle' });
await page.fill('input[type="url"]', 'https://localhost:8443');
await page.fill('input[type="text"]', user);
await page.fill('input[type="password"]', user);
await page.click('button[type="submit"]');
await page.waitForURL(/\/en(\/|$)/, { timeout: 60000 });
const gotIt = page.getByRole('button', { name: 'Got it' });
if (await gotIt.isVisible({ timeout: 3000 }).catch(() => false)) await gotIt.click();
await page.waitForSelector('[data-testid="email-list-item"]', { timeout: 30000 });

await page.keyboard.press('c');
await page.waitForSelector('[data-testid="email-composer"]', { timeout: 15000 });

await page.locator('[data-testid="composer-to"] input').fill(rcpt);
await page.locator('[data-testid="composer-to"] input').press('Tab');
await page.locator('input[data-testid="composer-subject"]').fill('Probe send ' + Date.now());
await page.locator('[data-testid="email-composer"] .ProseMirror').click();
await page.keyboard.type('Hello send probe body');
await page.waitForTimeout(800);

console.log('--- clicking send ---');
await page.locator('[data-testid="composer-send"]').last().click({ timeout: 10000 });
await page.waitForTimeout(6000);
console.log('composer visible after send:', await page.locator('[data-testid="email-composer"]').count());
await browser.close();