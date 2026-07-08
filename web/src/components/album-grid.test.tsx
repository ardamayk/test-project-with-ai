import type { Album } from "@repo/api-client";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AlbumGrid } from "./album-grid";

vi.mock("@tanstack/react-router", () => ({
	Link: ({
		children,
		to,
		params: _params,
		...props
	}: {
		children: React.ReactNode;
		to: string;
		params?: Record<string, string>;
	}) => (
		<a href={to} {...props}>
			{children}
		</a>
	),
	useNavigate: () => vi.fn(),
}));

vi.mock("#/hooks/use-delete-library", () => ({
	useDeleteAlbum: () => ({ mutate: vi.fn(), isPending: false }),
	confirmDelete: () => false,
}));

describe("AlbumGrid", () => {
	it("renders empty state", () => {
		render(<AlbumGrid albums={[]} />);
		expect(screen.getByText(/No albums yet/)).toBeTruthy();
	});

	it("uses the shared hover lift transition for album cards", () => {
		const albums = [
			{
				id: "a1",
				title: "Kind of Blue",
				artistName: "Miles Davis",
				trackCount: 5,
			},
		] as Album[];

		render(<AlbumGrid albums={albums} />);

		const album = screen.getByRole("link", { name: /Kind of Blue/ });
		expect(album.className).toContain("duration-300");
		expect(album.className).toContain("ease-out");
		expect(album.className).toContain("hover:-translate-y-1");
		expect(album.className).toContain("hover:shadow-lg");
	});
});
