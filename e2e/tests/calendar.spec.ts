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

  test('edits an existing calendar event through the UI and asserts the update', async ({ page }) => {
    const acct = uniqueUser('cal-edit');
    const jmap = await JMAPClient.connect(acct.username, acct.password);

    await login(page, acct.username, acct.password);
    await goToApp(page, '/en/calendar');

    // 1. Create initial event via UI
    await page.getByRole('button', { name: 'Create event' }).first().click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible({ timeout: 15_000 });
    const initialTitle = `Initial Event ${Date.now()}`;
    await dialog.getByPlaceholder('Title').fill(initialTitle);
    await dialog.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(dialog).toBeHidden({ timeout: 15_000 });

    // Verify initial event is visible on grid
    const eventElement = page.getByText(initialTitle).first();
    await expect(eventElement).toBeVisible({ timeout: 20_000 });

    // 2. Click the event to open the details dialog, then click "Edit event"
    await eventElement.click();
    await expect(page.getByRole('button', { name: 'Edit event' })).toBeVisible({ timeout: 15_000 });
    await page.getByRole('button', { name: 'Edit event' }).click();

    // 3. Edit the title in the editor dialog and save
    await expect(dialog.getByPlaceholder('Title')).toBeVisible({ timeout: 15_000 });
    const updatedTitle = `Edited Event ${Date.now()}`;
    await dialog.getByPlaceholder('Title').fill(updatedTitle);
    await dialog.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(dialog).toBeHidden({ timeout: 15_000 });

    // 4. Verify updated title is displayed in the UI
    await expect(page.getByText(updatedTitle).first()).toBeVisible({ timeout: 20_000 });

    // 5. Verify the update persisted over the JMAP protocol
    await expect
      .poll(async () => (await jmap.queryEventIds({ text: updatedTitle })).length, { timeout: 20_000 })
      .toBeGreaterThan(0);
    const ids = await jmap.queryEventIds({ text: updatedTitle });
    const [ev] = await jmap.getEvents(ids, ['title']);
    expect(ev.title).toBe(updatedTitle);
  });

  test('clicks predefined calendar entry, updates its start time, asserts over API, navigates away and revalidates in UI', async ({ page }) => {
    const acct = uniqueUser('cal-start-time');
    const jmap = await JMAPClient.connect(acct.username, acct.password);

    // 1. Create a predefined calendar entry for today so it is present in the current view
    const today = new Date();
    const yyyy = today.getFullYear();
    const mm = String(today.getMonth() + 1).padStart(2, '0');
    const dd = String(today.getDate()).padStart(2, '0');
    const dateStr = `${yyyy}-${mm}-${dd}`;
    const initialStart = `${dateStr}T10:00:00`;
    const newStartTime = '14:30';
    const expectedStart = `${dateStr}T${newStartTime}:00`;
    const title = `Predefined Meeting ${Date.now()}`;

    const eventId = await jmap.createEvent({
      title,
      start: initialStart,
      duration: 'PT1H',
    });

    // 2. Log in and go into the calendar
    await login(page, acct.username, acct.password);
    await goToApp(page, '/en/calendar');

    // 3. Click the predefined calendar entry and set a new start time on it
    const eventElement = page.getByText(title).first();
    await expect(eventElement).toBeVisible({ timeout: 20_000 });
    await eventElement.click();

    await expect(page.getByRole('button', { name: 'Edit event' })).toBeVisible({ timeout: 15_000 });
    await page.getByRole('button', { name: 'Edit event' }).click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible({ timeout: 15_000 });
    const startTimeInput = dialog.locator('input[type="time"]').first();
    await startTimeInput.fill(newStartTime);
    await dialog.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(dialog).toBeHidden({ timeout: 15_000 });
    await expect(page.getByText('Event updated').first()).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText('Failed to update event')).toBeHidden();

    // 4. Check using API that the start time is as expected
    await expect
      .poll(async () => {
        const [ev] = await jmap.getEvents([eventId], ['start']);
        return ev?.start;
      }, { timeout: 20_000 })
      .toContain(newStartTime);

    const [updatedEv] = await jmap.getEvents([eventId], ['start', 'title']);
    expect(updatedEv.title).toBe(title);
    expect(updatedEv.start).toBe(expectedStart);

    // 5. Go out of the calendar and inside again
    await goToApp(page, '/en/contacts');
    await goToApp(page, '/en/calendar');

    // 6. Validate the updated event and start time in the calendar UI
    const reloadedEvent = page.getByText(title).first();
    await expect(reloadedEvent).toBeVisible({ timeout: 20_000 });
    await reloadedEvent.click();
    await expect(page.getByRole('button', { name: 'Edit event' })).toBeVisible({ timeout: 15_000 });
    await page.getByRole('button', { name: 'Edit event' }).click();
    await expect(dialog).toBeVisible({ timeout: 15_000 });
    await expect(dialog.locator('input[type="time"]').first()).toHaveValue(newStartTime);
    await dialog.getByRole('button', { name: 'Cancel' }).first().click();
    await expect(dialog).toBeHidden({ timeout: 15_000 });
  });

  test('edits an existing event to be recurring with reminders and verifies via API and UI', async ({ page }) => {
    const acct = uniqueUser('cal-recur-edit');
    const jmap = await JMAPClient.connect(acct.username, acct.password);

    const today = new Date();
    const yyyy = today.getFullYear();
    const mm = String(today.getMonth() + 1).padStart(2, '0');
    const dd = String(today.getDate()).padStart(2, '0');
    const dateStr = `${yyyy}-${mm}-${dd}`;
    const initialStart = `${dateStr}T11:00:00`;
    const title = `Weekly Standup ${Date.now()}`;

    const eventId = await jmap.createEvent({
      title,
      start: initialStart,
      duration: 'PT30M',
    });

    await login(page, acct.username, acct.password);
    await goToApp(page, '/en/calendar');

    // Open event editor
    const eventElem = page.getByText(title).first();
    await expect(eventElem).toBeVisible({ timeout: 20_000 });
    await eventElem.click();
    await expect(page.getByRole('button', { name: 'Edit event' })).toBeVisible({ timeout: 15_000 });
    await page.getByRole('button', { name: 'Edit event' }).click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible({ timeout: 15_000 });

    // Select weekly recurrence and add a reminder
    await dialog.locator('select').nth(1).selectOption('weekly');
    await dialog.getByRole('button', { name: 'Add reminder' }).click();

    await dialog.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(dialog).toBeHidden({ timeout: 15_000 });
    await expect(page.getByText('Event updated').first()).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText('Failed to update event')).toBeHidden();

    // Verify over JMAP API
    await expect
      .poll(async () => {
        const [ev] = await jmap.getEvents([eventId], ['recurrenceRule', 'recurrenceRules', 'alerts']);
        return ev?.recurrenceRule?.frequency || ev?.recurrenceRules?.[0]?.frequency;
      }, { timeout: 20_000 })
      .toBe('weekly');

    const [ev] = await jmap.getEvents([eventId], ['recurrenceRule', 'recurrenceRules', 'alerts']);
    expect(ev.recurrenceRule?.frequency || ev.recurrenceRules?.[0]?.frequency).toBe('weekly');
    expect(Object.keys(ev.alerts || {}).length).toBeGreaterThan(0);

    // Reload and check UI
    await goToApp(page, '/en/contacts');
    await goToApp(page, '/en/calendar');

    const reloaded = page.getByText(title).first();
    await expect(reloaded).toBeVisible({ timeout: 20_000 });
    await reloaded.click();
    await expect(page.getByRole('button', { name: 'Edit event' })).toBeVisible({ timeout: 15_000 });
    await page.getByRole('button', { name: 'Edit event' }).click();
    await expect(dialog).toBeVisible({ timeout: 15_000 });
    await expect(dialog.locator('select').nth(1)).toHaveValue('weekly');
    await dialog.getByRole('button', { name: 'Cancel' }).first().click();
    await expect(dialog).toBeHidden({ timeout: 15_000 });
  });

  test('opens calendar share dialog, lists users and groups, and shares with Alice Smith', async ({ page }) => {
    const acct = uniqueUser('cal-share-test');
    await login(page, acct.username, acct.password);
    await goToApp(page, '/en/calendar');

    const personalCal = page.getByText('Personal Calendar').first();
    await expect(personalCal).toBeVisible({ timeout: 20_000 });
    await personalCal.click({ button: 'right' });

    const shareItem = page.getByRole('menuitem', { name: 'Share calendar' });
    await expect(shareItem).toBeVisible({ timeout: 15_000 });
    await shareItem.click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible({ timeout: 15_000 });

    const addBtn = dialog.getByRole('button', { name: 'Add person or group' });
    await expect(addBtn).toBeVisible({ timeout: 15_000 });
    await addBtn.click();

    // Verify groups and users are listed in the share picker
    const searchInput = dialog.getByPlaceholder(/search/i).or(dialog.locator('input[type="text"]')).first();
    if (await searchInput.isVisible()) {
      await searchInput.fill('All Staff');
      await expect(dialog.getByText('All Staff').first()).toBeVisible({ timeout: 10_000 });
      await searchInput.fill('Engineering Team');
      await expect(dialog.getByText('Engineering Team').first()).toBeVisible({ timeout: 10_000 });
      await searchInput.fill('Alice');
      await expect(dialog.getByText('Alice Smith').first()).toBeVisible({ timeout: 10_000 });
      await dialog.getByText('Alice Smith').first().click();
    } else {
      await expect(dialog.getByText('All Staff').first()).toBeVisible({ timeout: 10_000 });
      await expect(dialog.getByText('Engineering Team').first()).toBeVisible({ timeout: 10_000 });
      await expect(dialog.getByText('Alice Smith').first()).toBeVisible({ timeout: 10_000 });
      await dialog.getByText('Alice Smith').first().click();
    }

    // Verify Alice is added to share list in dialog
    await expect(dialog.locator('li', { hasText: 'Alice Smith' })).toBeVisible({ timeout: 10_000 });

    // Close dialog
    await dialog.getByRole('button', { name: 'Close' }).last().click();
    await expect(dialog).toBeHidden({ timeout: 15_000 });
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
