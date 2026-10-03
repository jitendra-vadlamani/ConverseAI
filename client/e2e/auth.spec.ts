import { test, expect } from '@playwright/test';
import { PASSWORD, signIn, uniqueEmail } from './helpers';

test('register, log out and log back in through the UI', async ({ page }) => {
  const email = uniqueEmail('ui-register');
  await page.goto('/');
  await expect(page).toHaveURL(/\/login$/);

  await page.getByRole('link', { name: /sign up|register|create/i }).click();
  // Wait for the route change, or the fill below can land on the login form.
  await expect(page.getByRole('heading', { name: 'Create Account' })).toBeVisible();
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password', { exact: true }).fill(PASSWORD);
  await page.getByLabel('Confirm Password').fill(PASSWORD);
  await page.getByRole('button', { name: /create account|sign up|register/i }).click();
  await expect(page.getByText('How can I help you today?')).toBeVisible();

  await page.getByRole('button', { name: 'Logout' }).click();
  await expect(page).toHaveURL(/\/login$/);

  await page.getByLabel('Email').fill(email.toUpperCase()); // emails are case-insensitive
  await page.getByLabel('Password').fill(PASSWORD);
  await page.getByRole('button', { name: /login|sign in/i }).click();
  await expect(page.getByText('How can I help you today?')).toBeVisible();
});

test('wrong password shows an error', async ({ page }) => {
  await page.goto('/login');
  await page.getByLabel('Email').fill(uniqueEmail('nobody'));
  await page.getByLabel('Password').fill('wrong-password');
  await page.getByRole('button', { name: /login|sign in/i }).click();
  await expect(page.getByText('invalid email or password')).toBeVisible();
});

test('deleting the account signs out and the login stops working', async ({ page }) => {
  const email = await signIn(page, 'delete-me');
  await page.getByRole('link', { name: 'Settings' }).click();
  await page.getByRole('button', { name: /Security/ }).click();
  await page.locator('#deletePassword').fill(PASSWORD);
  page.once('dialog', d => d.accept());
  await page.getByRole('button', { name: 'Delete my account and data' }).click();
  await expect(page).toHaveURL(/\/login$/);

  const login = await page.request.post('/api/auth/login', { data: { email, password: PASSWORD } });
  expect(login.status()).toBe(401);
});
