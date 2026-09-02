import { expect, test } from "@playwright/test";

test("albums page loads", async ({ page }) => {
	await page.goto("/library/albums");
	await expect(page.getByRole("heading", { name: "Albums" })).toBeVisible();
	await expect(
		page
			.getByRole("navigation")
			.filter({ has: page.getByRole("link", { name: "Albums" }) }),
	).toBeVisible();
});

test("navigation links render", async ({ page }) => {
	await page.goto("/library/albums");
	await expect(page.getByRole("link", { name: "Albums" })).toBeVisible();
	await expect(page.getByRole("link", { name: "Artists" })).toBeVisible();
	await expect(page.getByRole("link", { name: "Genres" })).toBeVisible();
	await expect(page.getByRole("link", { name: "Settings" })).toBeVisible();
	const queueHeading = page.getByRole("heading", { name: "Queue" });
	if (!(await queueHeading.isVisible())) {
		await page.getByRole("button", { name: "Toggle queue panel" }).click();
	}
	await expect(queueHeading).toBeVisible();
});

test("albums page shows search and filters", async ({ page }) => {
	await page.goto("/library/albums");
	await expect(page.getByPlaceholder("Search albums...")).toBeVisible();
	await page.getByRole("button", { name: "Filters" }).click();
	await expect(
		page.getByRole("dialog", { name: "Album filters" }),
	).toBeVisible();
});

test("artists page loads from sidebar", async ({ page }) => {
	await page.goto("/library/albums");
	await page.getByRole("link", { name: "Artists" }).click();
	await expect(page).toHaveURL(/\/library\/artists/);
	await expect(page.getByRole("heading", { name: "Artists" })).toBeVisible();
	await expect(page.getByPlaceholder("Search artists...")).toBeVisible();
});

test("now playing widget shows empty state", async ({ page }) => {
	await page.goto("/library/albums");
	await expect(page.getByText("Nothing playing").first()).toBeVisible();
});

test("root redirects to albums", async ({ page }) => {
	await page.goto("/");
	await expect(page).toHaveURL(/\/library\/albums/);
});

test("settings theme preset buttons", async ({ page }) => {
	await page.goto("/settings");
	await expect(page.getByRole("button", { name: "Earthly" })).toBeVisible();
	await expect(
		page.getByRole("button", { name: "Vintage Harbor" }),
	).toBeVisible();
	await expect(page.getByRole("button", { name: "Sage Hearth" })).toBeVisible();
});

test("product docs load through server module", async ({ page }) => {
	await page.goto("/docs/");
	await expect(
		page.getByRole("heading", { name: "Navidrome Replacement" }),
	).toBeVisible();
	await expect(page.getByRole("link", { name: "API Reference" })).toBeVisible();
});

test("swagger reference loads through docs module", async ({ page }) => {
	await page.goto("/api/docs");
	await expect(
		page.getByRole("heading", { name: "Earthly Audio API Reference" }),
	).toBeVisible();
	await expect(page.locator("#swagger-ui")).toBeVisible();
});
