import { test, expect } from '@playwright/test';
import { login, uniqueUser, goToApp, JMAPClient } from '../lib/helpers';

/**
 * Calendar scheduling end-to-end (Bulwark UI ↔ imap-jmap): Alice invites Bob, Bob
 * logs in and sees the still-unconfirmed invitation in his own calendar, accepts it
 * and logs out, then Alice logs in again and sees Bob's acceptance.
 *
 * Following the calendar suite's convention, the browser drives the real UX that has
 * proven selectors (login/logout per user, navigating to the calendar, the event
 * appearing in the month grid), while the invite creation, RSVP, and status
 * assertions go over the exact JMAP wire protocol the client uses
 * (draft-ietf-jmap-calendars-27 Section 5.9.2) so the scheduling is verified against
 * the running server, not a stub. Both users are on the same server, so the invite is
 * delivered into Bob's calendar and Bob's acceptance reflected into Alice's copy
 * (same-server implicit scheduling).
 */
test('alice invites bob; bob sees the unconfirmed event, accepts; alice sees it accepted', async ({
  browser,
}) => {
  const alice = uniqueUser('cal-invite-alice');
  const bob = uniqueUser('cal-invite-bob');

  const aliceJmap = await JMAPClient.connect(alice.username, alice.password);
  const bobJmap = await JMAPClient.connect(bob.username, bob.password);

  // Use a date in the current month so it renders in the default month grid.
  const now = new Date();
  const start = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-15T15:00:00`;
  const title = `Kickoff ${Date.now()}`;

  // 1. Alice creates the event and invites Bob (server sends scheduling messages).
  await aliceJmap.inviteEvent(
    { title, start, duration: 'PT1H', replyTo: { imip: `mailto:${alice.username}` } },
    {
      [alice.username]: { email: alice.username, roles: { owner: true } },
      [bob.username]: {
        email: bob.username,
        roles: { attendee: true },
        participationStatus: 'needs-action',
      },
    },
  );

  // 2. Bob logs in, opens the calendar, and sees the (still unconfirmed) invitation.
  const bobCtx = await browser.newContext({ ignoreHTTPSErrors: true });
  const bobPage = await bobCtx.newPage();
  await login(bobPage, bob.username, bob.password);
  await goToApp(bobPage, '/en/calendar');
  await expect(bobPage.getByText(title).first()).toBeVisible({ timeout: 20_000 });

  // The invitation is in Bob's calendar with participation still pending (over JMAP).
  const bobEvent = await bobJmap.eventByTitle(title);
  expect(bobEvent, 'Bob should have received the invitation in his calendar').toBeTruthy();
  expect(bobEvent.participants[bob.username].participationStatus).toBe('needs-action');

  // 3. Bob accepts, then logs out.
  await bobJmap.rsvp(bobEvent.id, bob.username, 'accepted');
  await expect
    .poll(async () => (await bobJmap.eventByTitle(title))?.participants?.[bob.username]?.participationStatus, {
      timeout: 15_000,
    })
    .toBe('accepted');
  await bobCtx.close(); // log out

  // 4. Alice logs in again and sees the event; her copy reflects Bob's acceptance.
  const aliceCtx = await browser.newContext({ ignoreHTTPSErrors: true });
  const alicePage = await aliceCtx.newPage();
  await login(alicePage, alice.username, alice.password);
  await goToApp(alicePage, '/en/calendar');
  await expect(alicePage.getByText(title).first()).toBeVisible({ timeout: 20_000 });

  await expect
    .poll(
      async () => (await aliceJmap.eventByTitle(title))?.participants?.[bob.username]?.participationStatus,
      { timeout: 20_000 },
    )
    .toBe('accepted');
  await aliceCtx.close();
});
