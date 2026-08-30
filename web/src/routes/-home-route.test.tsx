import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { HomePage } from "./-home-page";

vi.mock("#/components/scan-library-button", () => ({
	ScanLibraryButton: () => <button type="button">Scan library</button>,
}));

describe("home route", () => {
	afterEach(() => {
		cleanup();
	});

	it("hosts the temporary scan library action", () => {
		render(<HomePage />);

		expect(screen.getByRole("heading", { name: "Home" })).toBeTruthy();
		expect(screen.getByRole("button", { name: "Scan library" })).toBeTruthy();
	});
});
