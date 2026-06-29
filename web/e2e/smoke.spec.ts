import { expect, test } from '@playwright/test'

test('albums page loads', async ({ page }) => {
  await page.goto('/library/albums')
  await expect(page.getByRole('heading', { name: 'Albums' })).toBeVisible()
  await expect(page.getByText('Earthly Audio')).toBeVisible()
})

test('navigation links render', async ({ page }) => {
  await page.goto('/library/albums')
  await expect(page.getByRole('link', { name: 'Albums' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Artists' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Genres' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Settings' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Queue' })).toBeVisible()
})

test('albums page shows filters and scan', async ({ page }) => {
  await page.goto('/library/albums')
  await expect(page.getByPlaceholder('Search albums…')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Scan library' })).toBeVisible()
})

test('artists page loads from sidebar', async ({ page }) => {
  await page.goto('/library/albums')
  await page.getByRole('link', { name: 'Artists' }).click()
  await expect(page).toHaveURL(/\/library\/artists/)
  await expect(page.getByRole('heading', { name: 'Artists' })).toBeVisible()
  await expect(page.getByPlaceholder('Search artists…')).toBeVisible()
})

test('now playing widget shows empty state', async ({ page }) => {
  await page.goto('/library/albums')
  await expect(page.getByText('Nothing playing').first()).toBeVisible()
})

test('root redirects to albums', async ({ page }) => {
  await page.goto('/')
  await expect(page).toHaveURL(/\/library\/albums/)
})

test('settings theme preset buttons', async ({ page }) => {
  await page.goto('/settings')
  await expect(page.getByRole('button', { name: 'Earthly' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Vintage Harbor' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Sage Hearth' })).toBeVisible()
})
