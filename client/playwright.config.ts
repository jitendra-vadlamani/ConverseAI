import { defineConfig, devices } from '@playwright/test';

// Browser end-to-end tests. They run the real Go server (with the built
// client embedded) against MongoDB, MinIO and Chroma from
// docker-compose.ci.yml, and a fake model server (cmd/fakeollama), so no
// real model or internet access is needed.
//
//   docker compose -f ../docker-compose.ci.yml up -d
//   npm run build && npx playwright test
//
// Set E2E_BASE_URL to test an already-running server instead.
const external = process.env.E2E_BASE_URL;
const isCI = !!process.env.CI;
const port = 18090;

export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  expect: { timeout: 15_000 },
  workers: 2,
  retries: isCI ? 1 : 0,
  forbidOnly: isCI,
  reporter: isCI ? [['github'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: external ?? `http://127.0.0.1:${port}`,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: external
    ? undefined
    : [
        {
          command: 'go run ./cmd/fakeollama -addr 127.0.0.1:11998',
          cwd: '..',
          url: 'http://127.0.0.1:11998/api/version',
          reuseExistingServer: !isCI,
          timeout: 120_000,
        },
        {
          command: 'go run . serve',
          cwd: '..',
          url: `http://127.0.0.1:${port}/readyz`,
          reuseExistingServer: !isCI,
          timeout: 180_000,
          env: {
            PORT: String(port),
            APP_ENV: 'development',
            JWT_SECRET: 'playwright-secret-playwright-secret-0123',
            DB_ENCRYPTION_KEY: '0123456789abcdef0123456789abcdef',
            MONGO_URI: 'mongodb://127.0.0.1:27019',
            DB_NAME: `e2e_ui_${Date.now()}`,
            MINIO_ENDPOINT: '127.0.0.1:19000',
            MINIO_ROOT_USER: 'ciuser',
            MINIO_ROOT_PASSWORD: 'ci-password-123',
            MINIO_BUCKET: `e2e-ui-${Date.now()}`,
            CHROMA_URL: 'http://127.0.0.1:18000',
            OLLAMA_BASE_URL: 'http://127.0.0.1:11998',
            COOKIE_SECURE: 'false',
            LOGIN_RATE_PER_MIN: '1000',
            REGISTER_RATE_PER_MIN: '1000',
            COMPLETION_RATE_PER_MIN: '1000',
          },
        },
      ],
});
