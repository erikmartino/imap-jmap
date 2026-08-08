import { test, expect } from '@playwright/test';
import { login, uniqueUser, goToApp, JMAPClient } from '../lib/helpers';

/**
 * Calendar end-to-end coverage: the Bulwark webmail client driving the imap-jmap
 * server. UI steps use only selectors already proven by the mail/PIM suites
 * (sidebar calendar link, "Create event", the Month/Week/Day/Agenda view tabs);
 * everything else is asserted over the exact JMAP wire protocol the client uses,
 * so calendar behaviour is verified end-to-end against the running server rather
 * than a stub. The JMAP-only cases open no browser.
 */
test.describe('calendar (Bulwark UI ↔ imap-jmap over JMAP)', () => {
  // ---- UI ------------------------------------------------------------------

  test('calendar app loads with the seeded Personal Calendar and view controls', async ({ page }) => {
    await login(page, 'user@example.com', 'user@example.com');
    await goToApp(page, '/en/calendar');

    await expect(page.getByText('Personal Calendar').first()).toBeVisible({ timeout: 15_000 });
    await expect(page.getByRole('button', { name: 'Create event' }).first()).toBeVisible();
    for (const tab of ['Month', 'Week', 'Day', 'Agenda']) {
      await expect(page.getByRole('button', { name: tab, exact: true }).first()).toBeVisible();
    }
  });

  test('Create event opens the event editor', async ({ page }) => {
    const acct = uniqueUser('cal-ui');
    await login(page, acct.username, acct.password);
    await goToApp(page, '/en/calendar');

    await page.getByRole('button', { name: 'Create event' }).first().click();
    // The editor surfaces a Save action (and usually a dialog) once open.
    await expect(
      page
        .getByRole('dialog')
        .or(page.getByRole('button', { name: 'Save', exact: true }))
        .first(),
    ).toBeVisible({ timeout: 15_000 });
  });

  test('an event created over JMAP renders in the Bulwark calendar', async ({ page }) => {
    const acct = uniqueUser('cal-render');
    const jmap = await JMAPClient.connect(acct.username, acct.password);

    const title = `E2E Sync ${Date.now()}`;
    await jmap.createEvent({ title, start: todayAtNoon(), duration: 'PT1H' });

    await login(page, acct.username, acct.password);
    await goToApp(page, '/en/calendar');
    // The calendar opens on today; Day view lists today's events, where the
    // event was scheduled.
    await page.getByRole('button', { name: 'Day', exact: true }).first().click();
    await expect(page.getByText(title).first()).toBeVisible({ timeout: 20_000 });
  });

  // ---- Protocol (the exact wire API the client uses) -----------------------

  test('CalendarEvent lifecycle over JMAP: create → query → get → update → destroy', async () => {
    const acct = uniqueUser('cal-life');
    const jmap = await JMAPClient.connect(acct.username, acct.password);

    const title = `Lifecycle ${Date.now()}`;
    const id = await jmap.createEvent({ title, start: '2026-09-01T09:00:00', duration: 'PT30M' });

    expect(await jmap.queryEventIds({ text: title })).toContain(id);

    let [ev] = await jmap.getEvents([id], ['title', 'start', 'duration']);
    expect(ev.title).toBe(title);
    expect(ev.start).toBe('2026-09-01T09:00:00');
    expect(ev.duration).toBe('PT30M');

    await jmap.updateEvent(id, { title: `${title} (updated)` });
    [ev] = await jmap.getEvents([id], ['title']);
    expect(ev.title).toBe(`${title} (updated)`);

    await jmap.destroyEvent(id);
    expect(await jmap.getEvents([id])).toHaveLength(0);
  });

  test('recurrenceRules expand into occurrences (expandRecurrences)', async () => {
    const acct = uniqueUser('cal-rrule');
    const jmap = await JMAPClient.connect(acct.username, acct.password);

    const title = `Weekly ${Date.now()}`;
    const id = await jmap.createEvent({
      title,
      start: '2026-09-07T10:00:00',
      duration: 'PT1H',
      recurrenceRules: [{ '@type': 'RecurrenceRule', frequency: 'weekly', count: 5 }],
    });

    // Without expansion the query returns a single master id.
    expect(await jmap.queryEventIds({ text: title })).toEqual([id]);

    // With expansion the five weekly occurrences are returned, each derived from
    // the master id (evtId#recurrenceId).
    const expanded = await jmap.queryEventIds({ text: title }, { expandRecurrences: true });
    expect(expanded).toHaveLength(5);
    for (const occ of expanded) {
      expect(occ.startsWith(id)).toBeTruthy();
    }
  });

  test('owner sees their own private and secret events (privacy is only for sharees)', async () => {
    const acct = uniqueUser('cal-priv');
    const jmap = await JMAPClient.connect(acct.username, acct.password);

    const priv = await jmap.createEvent({ title: 'Doctor Appointment', privacy: 'private', start: '2026-09-10T09:00:00' });
    const secret = await jmap.createEvent({ title: 'Secret Launch', privacy: 'secret', start: '2026-09-10T14:00:00' });

    const got = await jmap.getEvents([priv, secret], ['id', 'title', 'privacy']);
    const byId = new Map(got.map((e: any) => [e.id, e]));

    // The owner MUST see both events in full — secret is not hidden, private is
    // not reduced to "Busy" (draft-ietf-jmap-calendars-27 Section 4.2.10).
    expect(byId.has(priv)).toBeTruthy();
    expect(byId.has(secret)).toBeTruthy();
    expect(byId.get(priv).title).toBe('Doctor Appointment');
    expect(byId.get(secret).title).toBe('Secret Launch');
  });
});

/** Today's date at 12:00 local, as a JSCalendar LocalDateTime (avoids day-boundary shifts). */
function todayAtNoon(): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T12:00:00`;
}
