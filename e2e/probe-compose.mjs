import { chromium } from '@playwright/test';

const base = 'http://localhost:3000';
const user = `sender-${Date.now()}@example.com`;

const browser = await chromium.launch({ headless: true });
const page = await (await browser.newContext({ ignoreHTTPSErrors: true })).newPage();

page.on('console', (msg) => {
  const t = msg.type();
  if (t === 'error' || t === 'warning') console.log('CONSOLE', t, ':', msg.text().slice(0, 400));
});
page.on('pageerror', (err) => console.log('PAGEERROR:', String(err).slice(0, 400)));

page.on('request', (req) => {
  if (req.method() !== 'POST') return;
  const d = req.postData() || '';
  const j = d.startsWith('{') ? JSON.parse(d) : null;
  const names = j?.methodCalls?.map((c) => c[0]).join(',') || req.resourceType();
  if (names.includes('Email')) console.log('>> POST', names, '|', d.slice(0, 300));
});
page.on('response', async (resp) => {
  if (resp.request().method() !== 'POST') return;
  const ct = resp.headers()['content-type'] || '';
  const url = resp.url();
  const d = resp.request().postData() || '';
  const j = d.startsWith('{') ? JSON.parse(d) : null;
  const names = j?.methodCalls?.map((c) => c[0]).join(',') || '';
  if (resp.status() >= 400) console.log('HTTP', resp.status(), url, names, '|', d.slice(0, 200));
  if (!ct.includes('json')) { console.log('POST resp (non-json)', resp.status(), url, names); return; }
  const body = await resp.text().catch(() => '');
  let json;
  try { json = JSON.parse(body); } catch { console.log('POST resp (bad json)', resp.status(), url); return; }
  for (const [name, args] of json.methodResponses || []) {
    if (name === 'Email/set') {
      console.log('Email/set FULL RESP:', JSON.stringify(args));
      continue;
    }
    if (name === 'error') { console.log('POST resp ERROR METHOD:', name, JSON.stringify(args)); continue; }
    const flags = [];
    for (const k of ['notCreated', 'notUpdated', 'notDestroyed', 'notFound', 'errors']) {
      if (args?.[k] && (Array.isArray(args[k]) ? args[k].length : Object.keys(args[k]).length)) flags.push(k + '=' + JSON.stringify(args[k]).slice(0, 150));
    }
    if (flags.length) console.log('POST resp', name, flags.join(' | '));
  }
});

await page.goto(base, { waitUntil: 'networkidle' });
await page.fill('input[type="url"]', 'https://localhost:8443');
await page.fill('input[type="text"]', user);
await page.fill('input[type="password"]', user);
await page.click('button[type="submit"]');

await page.waitForURL(/\/en(\/|$)/, { timeout: 60000 });
const gotIt = page.getByRole('button', { name: 'Got it' });
if (await gotIt.isVisible({ timeout: 3000 }).catch(() => false)) {
  await gotIt.click();
}
await page.waitForSelector('[data-testid="email-list-item"]', { timeout: 30000 }).catch(async () => {
  console.log('URL now:', page.url());
  console.log('BODY TEXT:\n', (await page.evaluate(() => document.body.innerText)).slice(0, 800));
  throw new Error('email-list-item never appeared');
});

await page.keyboard.press('c');
await page.waitForSelector('[data-testid="email-composer"]', { timeout: 15000 });
console.log('composer opened');

await page.locator('[data-testid="composer-to"] input').fill(`${user}`);
await page.locator('input[data-testid="composer-subject"]').fill(`Probe draft ${Date.now()}`);
await page.locator('[data-testid="email-composer"] .ProseMirror').click();
await page.keyboard.type('Hello probe body text');
await page.waitForTimeout(500);

console.log('save-status elements:');
const statuses = await page.locator('[data-testid="composer-save-status"]').count();
console.log('count:', statuses);
for (let i = 0; i < statuses; i++) {
  const el = page.locator('[data-testid="composer-save-status"]').nth(i);
  console.log('  ', await el.getAttribute('data-status'), JSON.stringify((await el.textContent() || '').trim()));
}

console.log('text around status (all [data-testid] in composer):');
const ids = await page.locator('[data-testid="email-composer"] [data-testid]').evaluateAll(els => els.map(e => e.getAttribute('data-testid')));
console.log('  ', ids);

// poll for saved state
for (let i = 0; i < 20; i++) {
  await page.waitForTimeout(500);
  const s = await page.locator('[data-testid="composer-save-status"]').getAttribute('data-status');
  if (s) {
    console.log('poll', i, 'status=', s, 'text=', JSON.stringify((await page.locator('[data-testid="composer-save-status"]').innerText().catch(() => ''))));
    if (s === 'saved') break;
  }
}

const body = await page.locator('[data-testid="email-composer"]').innerText().catch(() => '');
console.log('composer text contains save:', (body.match(/[Ss]av/g) || []).length);
console.log('composer text snippet:', body.slice(0, 300).replace(/\n+/g, ' | '));

await browser.close();