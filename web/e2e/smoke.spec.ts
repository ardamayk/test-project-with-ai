import { expect, test } from '@playwright/test'

test('home page loads with app title', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Navidrome Replacement' })).toBeVisible()
})

test('widget placeholders render', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByText('Now Playing (placeholder)')).toBeVisible()
})
