import { test, expect } from '@playwright/test';
import { send, signIn, waitForIdle } from './helpers';

test('streams a formatted answer that survives a reload', async ({ page }) => {
  await signIn(page, 'chat');
  await send(page, 'Say hello');

  const answer = page.locator('.message-wrapper.assistant').last();
  await expect(answer.locator('strong')).toHaveText('test model');
  await waitForIdle(page);
  // Markdown paragraphs arrive intact (newlines survived the stream).
  await expect(answer.locator('p')).toHaveCount(2);
  await expect(answer.locator('p').nth(1)).toHaveText('This is the second paragraph.');

  await page.reload();
  await page.getByText('Say hello').first().click();
  await expect(page.locator('.message-wrapper.user')).toHaveText(/Say hello/);
  await expect(page.locator('.message-wrapper.assistant strong')).toHaveText('test model');
});

test('stop ends the answer on the server and keeps the partial text', async ({ page }) => {
  await signIn(page, 'stop');
  await send(page, 'slow answer please');
  await expect(page.getByText(/word 3/)).toBeVisible();

  await page.getByRole('button', { name: 'Stop generating' }).click();
  await waitForIdle(page);
  const saved = page.locator('.message-wrapper.assistant').last();
  await expect(saved).toContainText('(stopped)');
  await expect(saved).not.toContainText('word 40');

  // The conversation accepts a new message right away (the run really ended).
  await send(page, 'Say hello');
  await expect(page.locator('.message-wrapper.assistant strong').last()).toHaveText('test model');
});

test('reloading mid-answer re-attaches and the answer completes', async ({ page }) => {
  await signIn(page, 'reattach');
  await send(page, 'slow story');
  await expect(page.getByText(/word 2/)).toBeVisible();

  await page.reload();
  await page.getByText('slow story').first().click();
  // Streaming continues after the reload and finishes.
  await expect(page.getByRole('button', { name: 'Stop generating' })).toBeVisible();
  await expect(page.locator('.message-wrapper.assistant').last()).toContainText('word 40', { timeout: 30_000 });
  await waitForIdle(page);
  await expect(page.locator('.message-wrapper.assistant')).toHaveCount(1);
});

test('uploads a file, answers from it and lists it in the Files tab', async ({ page }) => {
  await signIn(page, 'files');
  await page.locator('input[type=file]').setInputFiles({
    name: 'memo.txt', mimeType: 'text/plain', buffer: Buffer.from('The codeword is PELICAN.\nSecond line.'),
  });
  await expect(page.locator('.file-chip')).toContainText('memo.txt');
  await send(page, 'What does the memo say?');
  await expect(page.locator('.message-wrapper.assistant').last()).toContainText('I read your file: The codeword is PELICAN.');
  await waitForIdle(page);

  await page.getByRole('button', { name: 'Files', exact: true }).click();
  await expect(page.getByText('1 files')).toBeVisible();
  const card = page.locator('.files-grid .file-card');
  await expect(card).toContainText('memo.txt');

  // Download goes through the API (never a storage URL) with the session cookie.
  // The popup turns into a download and never changes its URL, so check the
  // request it makes instead.
  const download = page.context().waitForEvent('request', (r) => r.url().includes('/api/chat/files/download?fileID='));
  await card.getByText('Download').click();
  await download;
  const files: string[] = await (await page.request.get(`/api/chat/conversations/files?id=${await currentConversationId(page)}`)).json();
  const res = await page.request.get(`/api/chat/files/download?fileID=${encodeURIComponent(files[0])}`);
  expect(res.status()).toBe(200);
  expect(await res.text()).toContain('The codeword is PELICAN.');
});

test('System Logs show what happened during the answer', async ({ page }) => {
  await signIn(page, 'logs');
  await send(page, 'Say hello');
  await waitForIdle(page);
  await page.getByRole('button', { name: 'System Logs' }).click();
  await expect(page.getByText('User message received.')).toBeVisible();
  await expect(page.getByText(/Run succeeded/)).toBeVisible();
});

test('rates an answer', async ({ page }) => {
  await signIn(page, 'feedback');
  await send(page, 'Say hello');
  await waitForIdle(page);
  const good = page.getByRole('button', { name: 'Good answer' });
  await good.click();
  await expect(good).toHaveAttribute('aria-pressed', 'true');
});

test('another user cannot open my conversation', async ({ page, browser }) => {
  await signIn(page, 'owner');
  await send(page, 'Say hello');
  await waitForIdle(page);
  const convId = await currentConversationId(page);

  const other = await browser.newContext();
  const otherPage = await other.newPage();
  await signIn(otherPage, 'intruder');
  for (const path of [`/api/chat/conversations/get?id=${convId}`, `/api/chat/conversations/events?id=${convId}`]) {
    expect((await otherPage.request.get(path)).status()).toBe(404);
  }
  await expect(otherPage.locator('.conversation-item')).toHaveCount(0);
  await other.close();
});

async function currentConversationId(page: import('@playwright/test').Page): Promise<string> {
  const list: { id: string }[] = await (await page.request.get('/api/chat/conversations')).json();
  return list[0].id;
}
