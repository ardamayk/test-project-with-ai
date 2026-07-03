import type { RadioSearchResult, RadioStation } from "@repo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { matchesLocalStationFilter, RadioPage } from "./index";

const mocks = vi.hoisted(() => ({
	listRadioStations: vi.fn(),
	searchRadioStations: vi.fn(),
	createRadioStation: vi.fn(),
	importRadioStation: vi.fn(),
	patchRadioStation: vi.fn(),
	deleteRadioStation: vi.fn(),
	playRadioStation: vi.fn(),
}));

vi.mock("#/lib/api", () => ({
	apiClient: {
		listRadioStations: mocks.listRadioStations,
		searchRadioStations: mocks.searchRadioStations,
		createRadioStation: mocks.createRadioStation,
		importRadioStation: mocks.importRadioStation,
		patchRadioStation: mocks.patchRadioStation,
		deleteRadioStation: mocks.deleteRadioStation,
	},
}));

vi.mock("@repo/ui", () => ({
	usePlayback: () => ({
		playRadioStation: mocks.playRadioStation,
		currentRadioStation: null,
	}),
}));

vi.mock("@tanstack/react-router", async () => {
	const actual = await vi.importActual<typeof import("@tanstack/react-router")>(
		"@tanstack/react-router",
	);
	return {
		...actual,
		Link: ({
			children,
			to,
			params,
			...props
		}: {
			children: React.ReactNode;
			to: string;
			params?: Record<string, string>;
		}) => (
			<a href={params ? to.replace("$stationId", params.stationId) : to} {...props}>
				{children}
			</a>
		),
	};
});

const stations: RadioStation[] = [
	{
		id: "s1",
		name: "Jazz FM",
		streamUrl: "https://example.com/jazz",
		tags: ["jazz"],
		source: "manual",
		isFavorite: true,
		position: 0,
		country: "US",
	},
	{
		id: "s2",
		name: "Rock Hits",
		streamUrl: "https://example.com/rock",
		tags: ["rock"],
		source: "manual",
		isFavorite: false,
		position: 1,
		country: "UK",
	},
];

const searchResults: RadioSearchResult[] = [
	{
		stationUuid: "rb-1",
		name: "Ambient World",
		streamUrl: "https://example.com/ambient",
		tags: ["ambient"],
		country: "DE",
	},
];

function renderWithQuery(ui: React.ReactElement) {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	return render(
		<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>,
	);
}

describe("radio routes", () => {
	beforeEach(() => {
		mocks.listRadioStations.mockResolvedValue({
			items: stations,
			total: stations.length,
		});
		mocks.searchRadioStations.mockResolvedValue({
			items: searchResults,
			total: searchResults.length,
		});
		mocks.createRadioStation.mockResolvedValue(stations[0]);
		mocks.importRadioStation.mockResolvedValue(stations[0]);
		mocks.patchRadioStation.mockResolvedValue(stations[0]);
		mocks.deleteRadioStation.mockResolvedValue(undefined);
		mocks.playRadioStation.mockClear();
		mocks.patchRadioStation.mockClear();
		mocks.importRadioStation.mockClear();
	});

	afterEach(() => {
		cleanup();
	});

	it("matches saved stations against local filter text", () => {
		expect(matchesLocalStationFilter(stations[0], "jazz")).toBe(true);
		expect(matchesLocalStationFilter(stations[1], "jazz")).toBe(false);
		expect(matchesLocalStationFilter(stations[1], "uk")).toBe(true);
	});

	it("renders favorite and all station sections", async () => {
		renderWithQuery(<RadioPage />);

		expect(await screen.findByRole("heading", { name: "Favorites" })).toBeTruthy();
		expect(screen.getByRole("heading", { name: "All stations" })).toBeTruthy();
		expect(screen.getByText("Jazz FM")).toBeTruthy();
		expect(screen.getByText("Rock Hits")).toBeTruthy();
		expect(
			screen.getAllByRole("link", { name: "Details" })[0].getAttribute("href"),
		).toBe("/radio/s1");
	});

	it("filters saved stations locally", async () => {
		renderWithQuery(<RadioPage />);

		await screen.findByText("Jazz FM");

		fireEvent.change(screen.getByPlaceholderText("Filter saved stations"), {
			target: { value: "rock" },
		});

		expect(screen.queryByText("Jazz FM")).toBeNull();
		expect(screen.getByText("Rock Hits")).toBeTruthy();
		expect(screen.queryByRole("heading", { name: "Favorites" })).toBeNull();
	});

	it("plays and edits saved stations", async () => {
		renderWithQuery(<RadioPage />);

		await screen.findByText("Rock Hits");

		fireEvent.click(screen.getAllByRole("button", { name: "Play" })[1]);
		expect(mocks.playRadioStation).toHaveBeenCalledWith(stations[1]);

		fireEvent.click(screen.getAllByLabelText("Edit station")[1]);
		fireEvent.change(screen.getByDisplayValue("Rock Hits"), {
			target: { value: "Classic Rock" },
		});
		fireEvent.click(screen.getByRole("button", { name: "Save" }));

		await waitFor(() => {
			expect(mocks.patchRadioStation).toHaveBeenCalledWith("s2", {
				name: "Classic Rock",
				streamUrl: "https://example.com/rock",
			});
		});
	});

	it("imports Radio Browser search results", async () => {
		renderWithQuery(<RadioPage />);

		await screen.findByText("Jazz FM");

		fireEvent.change(screen.getByPlaceholderText("Search Radio Browser"), {
			target: { value: "ambient" },
		});
		fireEvent.click(screen.getByRole("button", { name: "Search" }));

		expect(await screen.findByText("Ambient World")).toBeTruthy();

		fireEvent.click(screen.getByRole("button", { name: "Import" }));

		await waitFor(() => {
			expect(mocks.importRadioStation).toHaveBeenCalledWith({
				result: searchResults[0],
			});
		});
	});
});
