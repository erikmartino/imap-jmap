import { test, expect } from '@playwright/test';
import { login, uniqueUser, goToApp, sendSmtp, JMAPClient } from '../lib/helpers';

/**
 * Opens the calendar, then delivers an iMIP invitation (RFC 6047 / RFC 5546
 * METHOD:REQUEST) straight to the imap-jmap SMTP receiver over a raw TCP socket — NOT
 * through Bulwark or JMAP — and confirms it shows up in the calendar.
 *
 * The invite is verified twice: first over JMAP (the server imported it into the
 * invitee's own calendar — the SMTP→calendar path, independent of the UI), then in the
 * Bulwark calendar grid after the view is refreshed.
 *
 * Note on "live": the server DOES emit a JMAP `StateChange` push on the import (the push
 * path is covered by the Go tests, and Bulwark holds an `/eventsource` connection), but
 * the Bulwark calendar grid does not currently re-query on that push — it refreshes on
 * (re)mount — so this test re-navigates to the calendar to pick the invite up rather than
 * asserting an instant in-place update.
 */
test('an iMIP invitation sent over SMTP appears in the calendar', async ({ page }) => {
  const invitee = uniqueUser('cal-smtp');

  await login(page, invitee.username, invitee.password);
  await goToApp(page, '/en/calendar');
  await expect(page).toHaveURL(/\/calendar\/month\//);

  // A floating (no-Z) start on the 15th of the current month renders in the default
  // month grid and matches Bulwark's floating LocalDateTime range query.
  const now = new Date();
  const stamp = `${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, '0')}15T090000`;
  const title = `SMTP Invite ${Date.now()}`;
  const uid = `smtp-invite-${Date.now()}@ext.test`;
  const organizer = 'organizer@ext.test';

  const message = [
    `From: Organizer <${organizer}>`,
    `To: <${invitee.username}>`,
    `Subject: Invitation: ${title}`,
    'MIME-Version: 1.0',
    'Content-Type: text/calendar; method=REQUEST; charset=UTF-8',
    '',
    'BEGIN:VCALENDAR',
    'VERSION:2.0',
    'PRODID:-//e2e//EN',
    'METHOD:REQUEST',
    'BEGIN:VEVENT',
    `UID:${uid}`,
    `DTSTAMP:${stamp}Z`,
    `DTSTART:${stamp}`,
    'DURATION:PT1H',
    `SUMMARY:${title}`,
    `ORGANIZER:mailto:${organizer}`,
    `ATTENDEE;CUTYPE=INDIVIDUAL;ROLE=REQ-PARTICIPANT;PARTSTAT=NEEDS-ACTION:mailto:${invitee.username}`,
    'END:VEVENT',
    'END:VCALENDAR',
  ].join('\r\n');

  // Deliver over SMTP (external path — no Bulwark, no JMAP).
  await sendSmtp(organizer, invitee.username, message);

  // 1) The server imported it into the invitee's calendar (SMTP → calendar), verified
  //    over the exact JMAP wire protocol the client uses.
  const jmap = await JMAPClient.connect(invitee.username, invitee.password);
  let imported: any = null;
  await expect
    .poll(async () => {
      imported = await jmap.eventByTitle(title);
      return imported != null;
    }, { timeout: 30_000 })
    .toBeTruthy();
  expect(imported.participants[invitee.username].participationStatus).toBe('needs-action');

  // 2) It appears in the calendar grid. Bulwark does not re-query the calendar on the
  //    JMAP push, so nudge the grid to re-mount by switching the view (Week → Month)
  //    within the still-open app, then assert the event chip is visible.
  await page.getByRole('button', { name: 'Week', exact: true }).first().click();
  await page.getByRole('button', { name: 'Month', exact: true }).first().click();
  // The grid renders the title in both a hidden measurement layer and the visible chip;
  // assert the visible one.
  const chip = page.getByText(title).filter({ visible: true }).first();
  await expect(chip).toBeVisible({ timeout: 20_000 });
});
