import { expect, request, type APIRequestContext, type Page } from '@playwright/test';

export const BASE_URL = process.env.BULWARK_BASE_URL ?? 'http://localhost:3000';
export const JMAP_URL = process.env.JMAP_SERVER_URL ?? 'https://localhost:8443';

const MAIL_CAPABILITY = 'urn:ietf:params:jmap:mail';
const CONTACTS_CAPABILITY = 'urn:ietf:params:jmap:contacts';
const CALENDAR_CAPABILITY = 'urn:ietf:params:jmap:calendars';
const CORE_CAPABILITY = 'urn:ietf:params:jmap:core';

let counter = 0;

/**
 * Returns a fresh, unique local account (username === password) that no other
 * test touches, so tests never race over shared mailbox state on the live
 * server. The imap-jmap MemoryAuthBackend accepts any username equal to its
 * password, and PrimaryDomainResolver treats every *@example.com as local.
 */
export function uniqueUser(prefix = 'bulwark-e2e'): { username: string; password: string } {
  counter += 1;
  const stamp = `${Date.now()}-${counter}-${Math.random().toString(36).slice(2, 8)}`;
  const username = `${prefix}-${stamp}@example.com`;
  return { username, password: username };
}

/** Wait for the Bulwark onboarding dialog and dismiss it if it appears. */
export async function dismissOnboarding(page: Page): Promise<void> {
  const dismiss = page.getByRole('button', { name: 'Got it' });
  if (await dismiss.isVisible({ timeout: 3000 }).catch(() => false)) {
    await dismiss.click();
  }
}

/**
 * Logs in through the Bulwark login form. The JMAP server URL field is filled
 * with JMAP_URL when the deployment allows custom endpoints (ALLOW_CUSTOM_JMAP_ENDPOINT);
 * env-locked deployments skip the field.
 */
export async function login(page: Page, username: string, password: string): Promise<void> {
  await page.goto(BASE_URL, { waitUntil: 'domcontentloaded' });
  const urlField = page.locator('input[type="url"]');
  await expect(urlField).toBeVisible({ timeout: 30_000 });
  if (await urlField.isEditable().catch(() => false)) {
    await urlField.fill(JMAP_URL);
  }
  await page.fill('input[type="text"]', username);
  await page.fill('input[type="password"]', password);
  await page.click('button[type="submit"]');
  await expect(page).toHaveURL(/\/en(\/|$)/, { timeout: 60_000 });
  await dismissOnboarding(page);
  await expect(page.locator('[data-testid="email-list-item"]').or(page.getByText('No emails yet')).first()).toBeVisible({
    timeout: 30_000,
  });
}

/** Navigates to another app section through the SPA sidebar (keeps the session). */
export async function goToApp(page: Page, path: string): Promise<void> {
  await page.locator(`a[href="${path}"]`).first().click();
  await page.waitForURL(`**${path}`, { timeout: 30_000 });
}

/** Opens the composer with the `c` shortcut and waits until it is ready. */
export async function openComposer(page: Page): Promise<void> {
  await page.keyboard.press('c');
  await expect(page.locator('[data-testid="email-composer"]')).toBeVisible({ timeout: 15_000 });
}

export interface EmailListItem {
  subject: string;
  preview: string;
  fromName: string;
}

/** Reads the sender/subject/preview of every visible message row. */
export async function visibleEmails(page: Page): Promise<EmailListItem[]> {
  const items = page.locator('[data-testid="email-list-item"]');
  const count = await items.count();
  const out: EmailListItem[] = [];
  for (let i = 0; i < count; i++) {
    const item = items.nth(i);
    out.push({
      subject: (await item.getAttribute('aria-label')) ?? (await item.innerText()),
      preview: await item.innerText(),
      fromName: '',
    });
  }
  return out;
}

/**
 * Minimal JMAP API client used to assert server state directly over the
 * protocol (RFC 8620), independent of the webmail UI: draft creation, delivery,
 * mailbox membership and keywords all verified against the wire protocol.
 */
export class JMAPClient {
  private readonly apiUrl: string;
  private readonly accountId: string;
  /** Account that owns the calendars capability; falls back to the mail account. */
  private calendarAccountId: string;
  private ctx: APIRequestContext | null = null;

  private constructor(apiUrl: string, accountId: string) {
    this.apiUrl = apiUrl;
    this.accountId = accountId;
    this.calendarAccountId = accountId;
  }

  static async connect(username: string, password: string): Promise<JMAPClient> {
    const ctx = await request.newContext({
      ignoreHTTPSErrors: true,
      baseURL: JMAP_URL,
      extraHTTPHeaders: {
        Authorization: `Basic ${Buffer.from(`${username}:${password}`).toString('base64')}`,
      },
    });
    const session = await (await ctx.get('/.well-known/jmap')).json();
    const primary = session.primaryAccounts?.[MAIL_CAPABILITY];
    if (!primary) {
      throw new Error('JMAP session exposes no mail account for ' + username);
    }
    const client = new JMAPClient(session.apiUrl, primary);
    client.calendarAccountId = session.primaryAccounts?.[CALENDAR_CAPABILITY] ?? primary;
    client.ctx = ctx;
    return client;
  }

  /** True when the server advertises the JMAP for Calendars capability in the session. */
  static async supportsCalendars(username: string, password: string): Promise<boolean> {
    const ctx = await request.newContext({
      ignoreHTTPSErrors: true,
      baseURL: JMAP_URL,
      extraHTTPHeaders: {
        Authorization: `Basic ${Buffer.from(`${username}:${password}`).toString('base64')}`,
      },
    });
    try {
      const session = await (await ctx.get('/.well-known/jmap')).json();
      return Boolean(session.capabilities?.[CALENDAR_CAPABILITY]);
    } finally {
      await ctx.dispose();
    }
  }

  async api(methods: Array<[string, Record<string, unknown>, string]>, opts?: { using?: string[] }): Promise<any> {
    if (!this.ctx) throw new Error('JMAPClient disposed');
    const using = opts?.using ?? [CORE_CAPABILITY, MAIL_CAPABILITY];
    const res = await this.ctx.post(this.apiUrl, {
      data: {
        using,
        methodCalls: methods,
      },
    });
    if (!res.ok()) {
      throw new Error(`JMAP API error HTTP ${res.status()}: ${await res.text()}`);
    }
    return (await res.json()).methodResponses;
  }

  /** Returns contact cards matching the given email address (RFC 9610). */
  async contactsByEmail(email: string): Promise<any[]> {
    const query = await this.callWith('ContactCard/query', { filter: { email } }, [CONTACTS_CAPABILITY]);
    const ids: string[] = query.ids ?? [];
    if (ids.length === 0) return [];
    const resp = await this.callWith('ContactCard/get', { ids }, [CONTACTS_CAPABILITY]);
    return resp.list ?? [];
  }

  private async callWith(method: string, args: Record<string, unknown>, using: string[]): Promise<any> {
    const responses = await this.api([[method, { accountId: this.accountId, ...args }, 'c0']], { using });
    return responses[0][1];
  }

  private async call(method: string, args: Record<string, unknown>): Promise<any> {
    const responses = await this.api([[method, { accountId: this.accountId, ...args }, 'c0']]);
    return responses[0][1];
  }

  async mailboxes(): Promise<any[]> {
    return (await this.call('Mailbox/get', {})).list;
  }

  async mailboxIdByRole(role: string): Promise<string | undefined> {
    return (await this.mailboxes()).find((mb: any) => mb.role === role)?.id;
  }

  /** Returns the ids of all emails in the given mailbox role (e.g. "inbox", "sent", "drafts"). */
  async emailsInMailboxRole(role: string): Promise<string[]> {
    const mailboxId = await this.mailboxIdByRole(role);
    if (!mailboxId) throw new Error(`no mailbox with role ${role}`);
    return (await this.call('Email/query', { filter: { inMailbox: mailboxId } })).ids ?? [];
  }

  async emailsByIds(ids: string[]): Promise<any[]> {
    if (ids.length === 0) return [];
    return (await this.call('Email/get', { ids, properties: ['subject', 'mailboxIds', 'keywords'] })).list;
  }

  async findEmailIdsBySubject(subject: string): Promise<string[]> {
    return (await this.call('Email/query', { filter: { subject } })).ids ?? [];
  }

  /**
   * Polls the account until an email with the given subject is present in the
   * given mailbox role (delivery is asynchronous from the client's perspective).
   */
  async waitForEmailInRole(subject: string, role: string, timeoutMs = 30_000): Promise<string> {
    const deadline = Date.now() + timeoutMs;
    const mbId = await this.mailboxIdByRole(role);
    if (!mbId) throw new Error(`no mailbox with role ${role}`);
    for (;;) {
      const ids = await this.findEmailIdsBySubject(subject);
      const inRole: string[] = [];
      for (const email of await this.emailsByIds(ids)) {
        if (email.mailboxIds?.[mbId]) inRole.push(email.id);
      }
      if (inRole.length > 0) return inRole[0];
      if (Date.now() > deadline) {
        throw new Error(`email "${subject}" not in ${role} within ${timeoutMs}ms`);
      }
      await new Promise((resolve) => setTimeout(resolve, 1000));
    }
  }

  async waitForNoEmail(subject: string, timeoutMs = 15_000): Promise<void> {
    const deadline = Date.now() + timeoutMs;
    for (;;) {
      const ids = await this.findEmailIdsBySubject(subject);
      if (ids.length === 0) return;
      if (Date.now() > deadline) {
        throw new Error(`email "${subject}" still present after ${timeoutMs}ms`);
      }
      await new Promise((resolve) => setTimeout(resolve, 500));
    }
  }

  // --- JMAP for Calendars (draft-ietf-jmap-calendars) -----------------------
  // These drive the exact wire protocol the Bulwark client uses, so calendar
  // behaviour is verified end-to-end against the running server, not a stub.

  private async callCalendar(method: string, args: Record<string, unknown>): Promise<any> {
    const responses = await this.api([[method, { accountId: this.calendarAccountId, ...args }, 'c0']], {
      using: [CORE_CAPABILITY, CALENDAR_CAPABILITY],
    });
    const [name, payload] = responses[0];
    if (name === 'error') {
      throw new Error(`${method} failed: ${JSON.stringify(payload)}`);
    }
    return payload;
  }

  /** Returns every Calendar object for the account (Calendar/get). */
  async calendars(): Promise<any[]> {
    return (await this.callCalendar('Calendar/get', {})).list ?? [];
  }

  /** Returns the id of the account's default calendar, if any. */
  async defaultCalendarId(): Promise<string | undefined> {
    return (await this.calendars()).find((c: any) => c.isDefault)?.id;
  }

  /**
   * Creates a CalendarEvent (CalendarEvent/set create) and returns its server id.
   * `props` is a JSCalendar Event object; calendarIds defaults to the default calendar.
   */
  async createEvent(props: Record<string, unknown>): Promise<string> {
    let event = props;
    if (!('calendarIds' in props)) {
      const calId = await this.defaultCalendarId();
      if (calId) event = { ...props, calendarIds: { [calId]: true } };
    }
    const res = await this.callCalendar('CalendarEvent/set', { create: { e0: event } });
    const created = res.created?.e0;
    if (!created) {
      throw new Error(`CalendarEvent/set did not create the event: ${JSON.stringify(res.notCreated)}`);
    }
    return created.id;
  }

  /** Applies a partial patch to an event (CalendarEvent/set update). */
  async updateEvent(id: string, patch: Record<string, unknown>): Promise<void> {
    const res = await this.callCalendar('CalendarEvent/set', { update: { [id]: patch } });
    if (res.notUpdated?.[id]) {
      throw new Error(`CalendarEvent/set update failed: ${JSON.stringify(res.notUpdated[id])}`);
    }
  }

  /** Destroys an event (CalendarEvent/set destroy). */
  async destroyEvent(id: string): Promise<void> {
    const res = await this.callCalendar('CalendarEvent/set', { destroy: [id] });
    if (res.notDestroyed?.[id]) {
      throw new Error(`CalendarEvent/set destroy failed: ${JSON.stringify(res.notDestroyed[id])}`);
    }
  }

  /** Fetches events by id (CalendarEvent/get), optionally limiting properties. */
  async getEvents(ids: string[], properties?: string[]): Promise<any[]> {
    if (ids.length === 0) return [];
    const args: Record<string, unknown> = { ids };
    if (properties) args.properties = properties;
    return (await this.callCalendar('CalendarEvent/get', args)).list ?? [];
  }

  /** Returns matching event ids (CalendarEvent/query). Pass extra args like expandRecurrences. */
  async queryEventIds(
    filter?: Record<string, unknown>,
    extra?: Record<string, unknown>,
  ): Promise<string[]> {
    const args: Record<string, unknown> = { ...extra };
    if (filter) args.filter = filter;
    return (await this.callCalendar('CalendarEvent/query', args)).ids ?? [];
  }
}
