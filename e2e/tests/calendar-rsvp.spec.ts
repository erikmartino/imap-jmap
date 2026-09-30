import { test, expect } from '@playwright/test';
import { login, uniqueUser, goToApp, JMAPClient } from '../lib/helpers';

/**
 * Calendar invite & RSVP end-to-end test with two accounts:
 * - Two accounts: Organizer and Invitee
 * - Event creation with invitation
 * - Invitee receives the invitation in their calendar (unconfirmed / needs-action)
 * - Invitee accepts the invitation (RSVP accepted)
 * - Validates that both the Invitee's and Organizer's calendar entries are updated
 *   to reflect the accepted participation status, both in the UI and over JMAP.
 */
test.describe('calendar scheduling between two accounts', () => {
  test('event creation, invite, acceptance and calendar entry updates for both accounts', async ({
    browser,
  }) => {
    test.setTimeout(120_000);

    const organizer = uniqueUser('cal-org');
    const invitee = uniqueUser('cal-inv');

    const orgJmap = await JMAPClient.connect(organizer.username, organizer.password);
    const invJmap = await JMAPClient.connect(invitee.username, invitee.password);

    // Pick a date in the current month so it renders in the default month grid
    const now = new Date();
    const start = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-15T14:00:00`;
    const title = `Planning Sync ${Date.now()}`;
    const description = 'Quarterly planning and project coordination';

    // 1. Organizer creates event with invitee attendee (sendSchedulingMessages: true)
    const { id: orgEventId, uid } = await orgJmap.inviteEvent(
      {
        title,
        description,
        start,
        duration: 'PT1H',
        replyTo: { imip: `mailto:${organizer.username}` },
      },
      {
        [organizer.username]: {
          email: organizer.username,
          roles: { owner: true },
          participationStatus: 'accepted',
        },
        [invitee.username]: {
          email: invitee.username,
          roles: { attendee: true },
          participationStatus: 'needs-action',
          expectReply: true,
        },
      },
    );
    expect(orgEventId).toBeTruthy();
    expect(uid).toBeTruthy();

    // 2. Invitee logs into Bulwark UI and sees the unconfirmed event on their calendar
    const invCtx = await browser.newContext({ ignoreHTTPSErrors: true });
    const invPage = await invCtx.newPage();
    await login(invPage, invitee.username, invitee.password);
    await goToApp(invPage, '/en/calendar');

    // The event appears in the Invitee's calendar grid
    const invEventLocator = invPage.getByText(title).first();
    await expect(invEventLocator).toBeVisible({ timeout: 20_000 });

    // Open event details dialog in Invitee UI
    await invEventLocator.click();
    const invDialog = invPage.getByRole('dialog');
    await expect(invDialog).toBeVisible({ timeout: 15_000 });
    await expect(invDialog.getByText(title).first()).toBeVisible();
    await invPage.keyboard.press('Escape');
    await expect(invDialog).toBeHidden({ timeout: 10_000 });

    // Verify over JMAP protocol that invitee's calendar has the entry with needs-action
    let invEvent: any = null;
    await expect
      .poll(async () => {
        invEvent = await invJmap.eventByTitle(title);
        return invEvent?.participants?.[invitee.username]?.participationStatus;
      }, { timeout: 20_000 })
      .toBe('needs-action');
    expect(invEvent.uid).toBe(uid);

    // 3. Invitee accepts the invitation (RSVP accepted)
    await invJmap.rsvp(invEvent.id, invitee.username, 'accepted');

    // 4. Validate Invitee's calendar entry is updated to accepted
    await expect
      .poll(async () => {
        const ev = await invJmap.eventByTitle(title);
        return ev?.participants?.[invitee.username]?.participationStatus;
      }, { timeout: 15_000 })
      .toBe('accepted');

    await invCtx.close();

    // 5. Validate Organizer's calendar entry is updated with Invitee's acceptance
    await expect
      .poll(async () => {
        const ev = await orgJmap.eventByTitle(title);
        return ev?.participants?.[invitee.username]?.participationStatus;
      }, { timeout: 20_000 })
      .toBe('accepted');

    // 6. Organizer logs into Bulwark UI and confirms the event is visible
    const orgCtx = await browser.newContext({ ignoreHTTPSErrors: true });
    const orgPage = await orgCtx.newPage();
    await login(orgPage, organizer.username, organizer.password);
    await goToApp(orgPage, '/en/calendar');

    const orgEventLocator = orgPage.getByText(title).first();
    await expect(orgEventLocator).toBeVisible({ timeout: 20_000 });

    // Open event details dialog in Organizer UI to assert updated details
    await orgEventLocator.click();
    const orgDialog = orgPage.getByRole('dialog');
    await expect(orgDialog).toBeVisible({ timeout: 15_000 });
    await expect(orgDialog.getByText(title).first()).toBeVisible();
    await orgPage.keyboard.press('Escape');

    // 7. Verify both calendar entries have matching UID and accepted statuses
    const finalOrgEvent = await orgJmap.eventByTitle(title);
    const finalInvEvent = await invJmap.eventByTitle(title);
    expect(finalOrgEvent.uid).toBe(finalInvEvent.uid);
    expect(finalOrgEvent.participants[invitee.username].participationStatus).toBe('accepted');
    expect(finalInvEvent.participants[invitee.username].participationStatus).toBe('accepted');

    await orgCtx.close();
  });
});
