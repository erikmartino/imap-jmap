import { test, expect } from '@playwright/test';
import { login, uniqueUser, goToApp } from '../lib/helpers';

test('probe: contacts toolbar buttons', async ({ page }) => {
  const user = uniqueUser('probe-contact');
  await login(page, user.username, user.password);
  await goToApp(page, '/en/contacts');
  await page.waitForTimeout(3000);
  const buttons = await page.getByRole('button').allInnerTexts();
  console.log('ALL BUTTONS:', JSON.stringify(buttons.filter((t) => t.trim() !== '')));
  const toolbar = page.locator('main, [class*="toolbar"], [class*="topbar"]').first();
  const html = await toolbar.innerHTML().catch(() => '<none>');
  console.log('TOOLBAR HTML:', html.slice(0, 2000));
  const links = await page.locator('a').allInnerTexts();
  console.log('ALL LINKS:', JSON.stringify(links));
  await page.waitForTimeout(2000);
});