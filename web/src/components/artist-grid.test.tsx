import type { Artist } from "@repo/api-client";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ArtistGrid } from "./artist-grid";

describe("ArtistGrid", () => {
	it("uses square cards in the shared collection grid without changing card behavior", () => {
		const artists = [
			{
				id: "artist-1",
				name: "Nina Simone",
				albumCount: 7,
			},
		] as Artist[];

		const { container } = render(<ArtistGrid artists={artists} />);

		const grid = container.firstElementChild;
		expect(grid?.className).toContain("grid-cols-2");
		expect(grid?.className).toContain("sm:grid-cols-3");
		expect(grid?.className).toContain("md:grid-cols-4");
		expect(grid?.className).toContain("lg:grid-cols-4");
		expect(grid?.className).toContain("xl:grid-cols-5");
		expect(grid?.className).toContain("gap-3");

		const card = screen.getByText("Nina Simone").closest("div");
		expect(card?.className).toContain("aspect-square");
		expect(card?.className).toContain("hover:bg-muted/50");
		expect(screen.getByText("N")).toBeTruthy();
		expect(screen.getByText("7 albums")).toBeTruthy();
		expect(screen.queryByRole("link")).toBeNull();
	});
});
