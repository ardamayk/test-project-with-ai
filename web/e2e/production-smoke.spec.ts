import { expect, test } from "@playwright/test";

test("production bundle renders with one Router context", async ({ page }) => {
	const pageErrors: string[] = [];
	page.on("pageerror", (error) => pageErrors.push(error.message));

	await page.goto("/library/albums");

	await expect(page.getByRole("link", { name: "Albums" })).toBeVisible();
	await expect(page.getByText("Something went wrong!")).toHaveCount(0);
	expect(pageErrors).not.toContainEqual(
		expect.stringMatching(/null.*stores|stores.*null/i),
	);
});
