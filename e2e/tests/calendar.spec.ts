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
    const user = uniqueUser('cal-user');
    await login(page, user.username, user.password);
    await goToApp(page, '/en/calendar');

    await expect(page.getByText('Personal Calendar').first()).toBeVisible({ timeout: 15_000 });
    await expect(page.getByRole('button', { name: 'Create event' }).first()).toBeVisible();
    for (const tab of ['Month', 'Week', 'Day', 'Agenda']) {
      await expect(page.getByRole('button', { name: tab, exact: true }).first()).toBeVisible();
    }
  });

  test('creates an event through the UI and reads it back over the protocol', async ({ page }) => {
    const acct = uniqueUser('cal-create');
    const jmap = await JMAPClient.connect(acct.username, acct.password);

    await login(page, acct.username, acct.password);
    await goToApp(page, '/en/calendar');

    // Open the event editor and fill the title (the date/time default to the
    // current view), then save.
    await page.getByRole('button', { name: 'Create event' }).first().click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible({ timeout: 15_000 });
    const title = `E2E Event ${Date.now()}`;
    await dialog.getByPlaceholder('Title').fill(title);
    await dialog.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(dialog).toBeHidden({ timeout: 15_000 });

    // The event is persisted server-side per the JMAP for Calendars I-D
    // (CalendarEvent/set → query/get), verified over the exact wire protocol.
    await expect
      .poll(async () => (await jmap.queryEventIds({ text: title })).length, { timeout: 20_000 })
      .toBeGreaterThan(0);
    const ids = await jmap.queryEventIds({ text: title });
    const [ev] = await jmap.getEvents(ids, ['title']);
    expect(ev.title).toBe(title);
  });

  test('a saved event appears in the month grid (regression: it used to disappear)', async ({ page }) => {
    // Regression for the calendar month view showing nothing after Save. The month grid
    // queries CalendarEvent/query with floating LocalDateTime before/after bounds
    // (draft-ietf-jmap-calendars-27 Section 5.11.1); the server previously required a "Z"
    // suffix and so returned no events, making saved entries vanish from the view.
    const acct = uniqueUser('cal-month');
    await login(page, acct.username, acct.password);
    await goToApp(page, '/en/calendar');
    const monthLabel = new Date().toLocaleDateString('en-US', {
      weekday: 'long',
      month: 'long',
      day: 'numeric',
      year: 'numeric',
    });
    await expect(page.getByRole('grid', { name: monthLabel })).toBeVisible({ timeout: 15_000 });

    await page.getByRole('button', { name: 'Create event' }).first().click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible({ timeout: 15_000 });
    const title = `Month Grid ${Date.now()}`;
    await dialog.getByPlaceholder('Title').fill(title);
    await dialog.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(dialog).toBeHidden({ timeout: 15_000 });

    // The event must remain visible in the month grid after saving.
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
