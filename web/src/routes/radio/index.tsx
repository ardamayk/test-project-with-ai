import type { RadioSearchResult, RadioStation } from "@repo/api-client";
import { usePlayback } from "@repo/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { Pencil, Play, Plus, Radio, Search, Star, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { Badge } from "#/components/ui/badge";
import { Button } from "#/components/ui/button";
import { Input } from "#/components/ui/input";
import { apiClient } from "#/lib/api";

const radioQueryKeys = {
	stations: ["radio", "stations"] as const,
	search: (query: string) => ["radio", "search", query] as const,
};

export const Route = createFileRoute("/radio/")({
	component: RadioPage,
});

export function matchesLocalStationFilter(
	station: RadioStation,
	filter: string,
): boolean {
	const query = filter.trim().toLowerCase();
	if (!query) {
		return true;
	}

	const haystack = [
		station.name,
		station.country,
		station.language,
		...station.tags,
	]
		.filter(Boolean)
		.join(" ")
		.toLowerCase();

	return haystack.includes(query);
}

export function RadioPage() {
	const queryClient = useQueryClient();
	const { playRadioStation, currentRadioStation } = usePlayback();
	const [manualName, setManualName] = useState("");
	const [manualStreamUrl, setManualStreamUrl] = useState("");
	const [searchInput, setSearchInput] = useState("");
	const [searchQuery, setSearchQuery] = useState("");
	const [localFilter, setLocalFilter] = useState("");

	const stations = useQuery({
		queryKey: radioQueryKeys.stations,
		queryFn: () => apiClient.listRadioStations(),
	});

	const search = useQuery({
		queryKey: radioQueryKeys.search(searchQuery),
		queryFn: () => apiClient.searchRadioStations({ q: searchQuery }),
		enabled: searchQuery.length >= 2,
	});

	const createStation = useMutation({
		mutationFn: () =>
			apiClient.createRadioStation({
				name: manualName.trim(),
				streamUrl: manualStreamUrl.trim(),
			}),
		onSuccess: async () => {
			setManualName("");
			setManualStreamUrl("");
			await queryClient.invalidateQueries({
				queryKey: radioQueryKeys.stations,
			});
		},
	});

	const importStation = useMutation({
		mutationFn: (result: RadioSearchResult) =>
			apiClient.importRadioStation({ result }),
		onSuccess: async () => {
			await queryClient.invalidateQueries({
				queryKey: radioQueryKeys.stations,
			});
		},
	});

	const updateStation = useMutation({
		mutationFn: ({
			stationId,
			...patch
		}: {
			stationId: string;
			name?: string;
			streamUrl?: string;
			isFavorite?: boolean;
		}) => apiClient.patchRadioStation(stationId, patch),
		onSuccess: async () => {
			await queryClient.invalidateQueries({
				queryKey: radioQueryKeys.stations,
			});
		},
	});

	const deleteStation = useMutation({
		mutationFn: (stationId: string) => apiClient.deleteRadioStation(stationId),
		onSuccess: async () => {
			await queryClient.invalidateQueries({
				queryKey: radioQueryKeys.stations,
			});
		},
	});

	const stationItems = stations.data?.items ?? [];
	const filteredStations = useMemo(
		() =>
			stationItems.filter((station) =>
				matchesLocalStationFilter(station, localFilter),
			),
		[localFilter, stationItems],
	);
	const favoriteStations = useMemo(
		() => filteredStations.filter((station) => station.isFavorite),
		[filteredStations],
	);
	const otherStations = useMemo(
		() => filteredStations.filter((station) => !station.isFavorite),
		[filteredStations],
	);
	const hasManualInput = manualName.trim() && manualStreamUrl.trim();
	const isStationMutating =
		updateStation.isPending || deleteStation.isPending;

	return (
		<div className="flex min-h-0 flex-1 flex-col gap-6 overflow-auto p-6">
			<header className="flex flex-col gap-2">
				<h1 className="font-semibold text-2xl text-heading">Radio</h1>
				<p className="max-w-2xl text-foreground text-sm">
					Add stream URLs directly or import stations from Radio Browser.
				</p>
			</header>

			<section className="grid gap-3 rounded-lg border border-border bg-card/35 p-4 lg:grid-cols-[minmax(0,1fr)_auto]">
				<div className="grid min-w-0 gap-3 sm:grid-cols-2">
					<label
						className="flex min-w-0 flex-col gap-1.5"
						htmlFor="manual-radio-name"
					>
						<span className="font-medium text-caption text-xs">Name</span>
						<Input
							id="manual-radio-name"
							value={manualName}
							onChange={(event) => setManualName(event.target.value)}
							placeholder="Station name"
						/>
					</label>
					<label
						className="flex min-w-0 flex-col gap-1.5"
						htmlFor="manual-radio-stream-url"
					>
						<span className="font-medium text-caption text-xs">Stream URL</span>
						<Input
							id="manual-radio-stream-url"
							value={manualStreamUrl}
							onChange={(event) => setManualStreamUrl(event.target.value)}
							placeholder="https://example.com/live"
							type="url"
						/>
					</label>
				</div>
				<Button
					className="self-end"
					disabled={!hasManualInput || createStation.isPending}
					onClick={() => createStation.mutate()}
				>
					<Plus className="size-4" />
					Add station
				</Button>
				{createStation.isError ? (
					<p className="text-destructive text-sm lg:col-span-2">
						Failed to add radio station
					</p>
				) : null}
			</section>

			<section className="flex flex-col gap-3">
				<div className="flex flex-col gap-2 sm:flex-row">
					<div className="relative min-w-0 flex-1">
						<Search className="-translate-y-1/2 pointer-events-none absolute top-1/2 left-3 size-4 text-caption" />
						<Input
							className="pl-9"
							value={searchInput}
							onChange={(event) => setSearchInput(event.target.value)}
							placeholder="Search Radio Browser"
							type="search"
							onKeyDown={(event) => {
								if (event.key === "Enter") {
									setSearchQuery(searchInput.trim());
								}
							}}
						/>
					</div>
					<Button
						variant="outline"
						disabled={searchInput.trim().length < 2}
						onClick={() => setSearchQuery(searchInput.trim())}
					>
						<Search className="size-4" />
						Search
					</Button>
				</div>

				{searchQuery ? (
					<div className="grid gap-2">
						{search.isLoading ? (
							<p className="text-foreground text-sm">Searching stations...</p>
						) : null}
						{search.isError ? (
							<p className="text-destructive text-sm">
								Failed to search radio stations
							</p>
						) : null}
						{(search.data?.items ?? []).slice(0, 12).map((result) => (
							<SearchResultRow
								key={result.stationUuid}
								result={result}
								isImporting={importStation.isPending}
								onImport={() => importStation.mutate(result)}
							/>
						))}
						{search.data && search.data.items.length === 0 ? (
							<p className="text-caption text-sm">No stations found.</p>
						) : null}
					</div>
				) : null}
			</section>

			<section className="flex min-h-0 flex-col gap-4">
				<div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
					<h2 className="font-semibold text-heading text-lg">Saved stations</h2>
					<div className="flex min-w-0 flex-1 items-center gap-2 sm:max-w-md">
						<div className="relative min-w-0 flex-1">
							<Search className="-translate-y-1/2 pointer-events-none absolute top-1/2 left-3 size-4 text-caption" />
							<Input
								className="pl-9"
								value={localFilter}
								onChange={(event) => setLocalFilter(event.target.value)}
								placeholder="Filter saved stations"
								type="search"
							/>
						</div>
						<Badge variant="outline">{filteredStations.length} shown</Badge>
					</div>
				</div>

				{stations.isLoading ? (
					<p className="text-foreground text-sm">Loading radio stations...</p>
				) : null}
				{stations.isError ? (
					<p className="text-destructive text-sm">
						Failed to load radio stations
					</p>
				) : null}
				{!stations.isLoading && stationItems.length === 0 ? (
					<div className="rounded-lg border border-dashed border-border p-6 text-caption text-sm">
						No saved stations yet.
					</div>
				) : null}
				{!stations.isLoading &&
				stationItems.length > 0 &&
				filteredStations.length === 0 ? (
					<div className="rounded-lg border border-dashed border-border p-6 text-caption text-sm">
						No saved stations match this filter.
					</div>
				) : null}

				{favoriteStations.length > 0 ? (
					<StationSection
						title="Favorites"
						stations={favoriteStations}
						currentRadioStationId={currentRadioStation?.id}
						isMutating={isStationMutating}
						onPlay={(station) => void playRadioStation(station)}
						onToggleFavorite={(station) =>
							updateStation.mutate({
								stationId: station.id,
								isFavorite: !station.isFavorite,
							})
						}
						onUpdate={(station, patch) =>
							updateStation.mutate({ stationId: station.id, ...patch })
						}
						onDelete={(stationId) => deleteStation.mutate(stationId)}
					/>
				) : null}

				{otherStations.length > 0 ? (
					<StationSection
						title="All stations"
						stations={otherStations}
						currentRadioStationId={currentRadioStation?.id}
						isMutating={isStationMutating}
						onPlay={(station) => void playRadioStation(station)}
						onToggleFavorite={(station) =>
							updateStation.mutate({
								stationId: station.id,
								isFavorite: !station.isFavorite,
							})
						}
						onUpdate={(station, patch) =>
							updateStation.mutate({ stationId: station.id, ...patch })
						}
						onDelete={(stationId) => deleteStation.mutate(stationId)}
					/>
				) : null}
			</section>
		</div>
	);
}

function StationSection({
	title,
	stations,
	currentRadioStationId,
	isMutating,
	onPlay,
	onToggleFavorite,
	onUpdate,
	onDelete,
}: {
	title: string;
	stations: RadioStation[];
	currentRadioStationId?: string;
	isMutating: boolean;
	onPlay: (station: RadioStation) => void;
	onToggleFavorite: (station: RadioStation) => void;
	onUpdate: (
		station: RadioStation,
		patch: { name: string; streamUrl: string },
	) => void;
	onDelete: (stationId: string) => void;
}) {
	return (
		<div className="flex flex-col gap-3">
			<h3 className="font-medium text-heading text-sm">{title}</h3>
			<div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
				{stations.map((station) => (
					<StationCard
						key={station.id}
						station={station}
						isActive={currentRadioStationId === station.id}
						isMutating={isMutating}
						onPlay={() => onPlay(station)}
						onToggleFavorite={() => onToggleFavorite(station)}
						onUpdate={(patch) => onUpdate(station, patch)}
						onDelete={() => onDelete(station.id)}
					/>
				))}
			</div>
		</div>
	);
}

function StationCard({
	station,
	isActive,
	isMutating,
	onPlay,
	onToggleFavorite,
	onUpdate,
	onDelete,
}: {
	station: RadioStation;
	isActive: boolean;
	isMutating: boolean;
	onPlay: () => void;
	onToggleFavorite: () => void;
	onUpdate: (patch: { name: string; streamUrl: string }) => void;
	onDelete: () => void;
}) {
	const [isEditing, setIsEditing] = useState(false);
	const [editName, setEditName] = useState(station.name);
	const [editStreamUrl, setEditStreamUrl] = useState(station.streamUrl);
	const canSaveEdit = editName.trim() && editStreamUrl.trim();

	return (
		<article className="flex min-w-0 flex-col gap-3 rounded-lg border border-border bg-card/40 p-4">
			{isEditing ? (
				<div className="grid gap-2">
					<label className="flex min-w-0 flex-col gap-1">
						<span className="font-medium text-caption text-xs">Name</span>
						<Input
							value={editName}
							onChange={(event) => setEditName(event.target.value)}
						/>
					</label>
					<label className="flex min-w-0 flex-col gap-1">
						<span className="font-medium text-caption text-xs">Stream URL</span>
						<Input
							value={editStreamUrl}
							onChange={(event) => setEditStreamUrl(event.target.value)}
							type="url"
						/>
					</label>
				</div>
			) : (
				<div className="flex min-w-0 items-center gap-3">
					<StationArtwork faviconUrl={station.faviconUrl} name={station.name} />
					<div className="min-w-0 flex-1">
						<p className="truncate font-medium text-heading" title={station.name}>
							{station.name}
						</p>
						<p className="truncate text-caption text-xs">
							{formatStationMeta(station)}
						</p>
					</div>
				</div>
			)}

			{!isEditing && station.lastNowPlaying?.raw ? (
				<p className="truncate text-foreground text-sm">
					{station.lastNowPlaying.raw}
				</p>
			) : null}

			<div className="flex items-center justify-between gap-2">
				{isEditing ? (
					<div className="flex items-center gap-2">
						<Button
							size="sm"
							disabled={!canSaveEdit || isMutating}
							onClick={() => {
								onUpdate({
									name: editName.trim(),
									streamUrl: editStreamUrl.trim(),
								});
								setIsEditing(false);
							}}
						>
							Save
						</Button>
						<Button
							size="sm"
							variant="outline"
							disabled={isMutating}
							onClick={() => {
								setEditName(station.name);
								setEditStreamUrl(station.streamUrl);
								setIsEditing(false);
							}}
						>
							Cancel
						</Button>
					</div>
				) : (
					<Button size="sm" onClick={onPlay}>
						<Play className="size-4" />
						{isActive ? "Playing" : "Play"}
					</Button>
				)}
				<div className="flex items-center gap-1">
					{!isEditing ? (
						<Link
							to="/radio/$stationId"
							params={{ stationId: station.id }}
							className="px-2 text-foreground text-sm hover:text-heading"
						>
							Details
						</Link>
					) : null}
					{!isEditing ? (
						<Button
							aria-label="Edit station"
							disabled={isMutating}
							size="icon-sm"
							variant="ghost"
							onClick={() => setIsEditing(true)}
						>
							<Pencil className="size-4" />
						</Button>
					) : null}
					<Button
						aria-label={station.isFavorite ? "Remove favorite" : "Add favorite"}
						disabled={isMutating}
						size="icon-sm"
						variant="ghost"
						onClick={onToggleFavorite}
					>
						<Star
							className={
								station.isFavorite
									? "size-4 fill-current text-heading"
									: "size-4"
							}
						/>
					</Button>
					<Button
						aria-label="Delete station"
						disabled={isMutating}
						size="icon-sm"
						variant="ghost"
						onClick={onDelete}
					>
						<Trash2 className="size-4" />
					</Button>
				</div>
			</div>
		</article>
	);
}

function SearchResultRow({
	result,
	isImporting,
	onImport,
}: {
	result: RadioSearchResult;
	isImporting: boolean;
	onImport: () => void;
}) {
	return (
		<div className="flex min-w-0 items-center gap-3 rounded-lg border border-border bg-card/25 p-3">
			<StationArtwork faviconUrl={result.faviconUrl} name={result.name} />
			<div className="min-w-0 flex-1">
				<p className="truncate font-medium text-heading" title={result.name}>
					{result.name}
				</p>
				<p className="truncate text-caption text-xs">
					{formatStationMeta(result)}
				</p>
			</div>
			<Button
				disabled={isImporting}
				size="sm"
				variant="outline"
				onClick={onImport}
			>
				<Plus className="size-4" />
				Import
			</Button>
		</div>
	);
}

function StationArtwork({
	faviconUrl,
	name,
}: {
	faviconUrl?: string;
	name: string;
}) {
	return faviconUrl ? (
		<img
			alt={name}
			className="size-12 shrink-0 rounded-md border border-border bg-muted object-cover"
			src={faviconUrl}
		/>
	) : (
		<div
			aria-hidden
			className="flex size-12 shrink-0 items-center justify-center rounded-md border border-border bg-muted text-caption"
		>
			<Radio className="size-5" />
		</div>
	);
}

function formatStationMeta(
	station: Pick<
		RadioStation,
		"bitrate" | "codec" | "country" | "language" | "tags"
	>,
): string {
	const location = [station.country, station.language]
		.filter(Boolean)
		.join(" / ");
	const quality = [
		station.codec ? station.codec.toUpperCase() : null,
		station.bitrate ? `${station.bitrate} kbps` : null,
	]
		.filter(Boolean)
		.join(" · ");
	const tags = station.tags.slice(0, 2).join(", ");
	return [location, quality, tags].filter(Boolean).join(" · ") || "Live radio";
}
