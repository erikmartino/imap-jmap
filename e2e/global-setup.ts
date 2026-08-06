import type { FullConfig } from '@playwright/test';
import { request } from '@playwright/test';

export default async function globalSetup(config: FullConfig) {
  const baseURL = config.projects[0]?.use?.baseURL ?? process.env.BULWARK_BASE_URL ?? 'http://localhost:3000';
  const jmapURL = process.env.JMAP_SERVER_URL ?? 'http://localhost:8080';

  const ctx = await request.newContext({ ignoreHTTPSErrors: true });
  try {
    const bulwark = await ctx.get(baseURL, { timeout: 10_000 });
    if (bulwark.status() >= 400 && bulwark.status() !== 401 && bulwark.status() !== 307) {
      throw new Error(`Bulwark webmail at ${baseURL} is not reachable (HTTP ${bulwark.status()}). Start it with: pnpm docker:up (or docker compose -f docker-compose.bulwark.yml up -d)`);
    }

    const jmap = await ctx.get(`${jmapURL}/.well-known/jmap`, { timeout: 10_000 });
    if (jmap.status() !== 401) {
      throw new Error(`JMAP server at ${jmapURL}/.well-known/jmap did not require authentication (HTTP ${jmap.status()}). Start it with: pnpm docker:up (or docker compose -f docker-compose.bulwark.yml up -d)`);
    }
  } finally {
    await ctx.dispose();
  }
}
