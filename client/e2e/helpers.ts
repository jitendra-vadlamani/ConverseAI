import { expect, type Page } from '@playwright/test';

export const PASSWORD = 'password-123';

export function uniqueEmail(prefix: string): string {
  return `${prefix}-${Date.now()}-${Math.floor(Math.random() * 1e6)}@example.com`;
}

// Registers and logs in through the API. page.request shares cookies with
// the browser context, so the page is signed in afterwards.
export async function signIn(page: Page, prefix = 'user'): Promise<string> {
  const email = uniqueEmail(prefix);
  const reg = await page.request.post('/api/auth/register', { data: { email, password: PASSWORD } });
  expect(reg.status()).toBe(201);
  const login = await page.request.post('/api/auth/login', { data: { email, password: PASSWORD } });
  expect(login.status()).toBe(200);
  await page.goto('/');
  await expect(page.getByText('How can I help you today?')).toBeVisible();
  return email;
}

export async function send(page: Page, text: string) {
  await page.getByRole('textbox', { name: 'Message' }).fill(text);
  await page.getByRole('textbox', { name: 'Message' }).press('Enter');
}

export async function waitForIdle(page: Page) {
  await expect(page.getByRole('button', { name: 'Send message' })).toBeVisible({ timeout: 30_000 });
}
