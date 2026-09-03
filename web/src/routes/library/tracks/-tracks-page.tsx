import { MANAGED_IMPORT_CAPABILITY, type Track } from "@repo/api-client";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Search } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import {
	COLLECTION_PAGE_CONTAINER_CLASS,
	CollectionPageContainer,
} from "#/components/collection-grid-layout";
import { PageHeader, PageShell } from "#/components/page-layout";
import { TrackList } from "#/components/track-list";
import { Button } from "#/components/ui/button";
import { Input } from "#/components/ui/input";
import { useReturnFocus } from "#/hooks/use-return-focus";
import {
	type ServerCapabilityState,
	useServerCapabilityState,
} from "#/hooks/use-server-capability";
import { apiClient } from "#/lib/api";
import { filterTracksByText } from "#/lib/filter-tracks";
import { ImportHistory } from "./-import-history";
import { ImportMusicDialog } from "./-import-music-dialog";
import { LegacyMigrationBanner } from "./-legacy-migration-banner";

function useDebouncedValue<T>(value: T, delayMs: number): T {
	const [debounced, setDebounced] = useState(value);

	useEffect(() => {
		const timeout = window.setTimeout(() => setDebounced(value), delayMs);
		return () => window.clearTimeout(timeout);
	}, [value, delayMs]);

	return debounced;
}

export function TracksPage() {
	const tracks = useTrackSearch();
	const managedImport = useManagedImport();
	const importCapability = useServerCapabilityState(MANAGED_IMPORT_CAPABILITY);

	return (
		<PageShell
			testId="tracks-page-shell"
			contentTestId="tracks-page-content"
			header={
				<TracksHeader
					search={tracks.search}
					onSearchChange={tracks.setSearch}
					onImport={managedImport.open}
					importCapability={importCapability}
				/>
			}
		>
			<CollectionPageContainer className="space-y-6">
				<LegacyMigrationBanner tracks={tracks.sourceTracks} />
				<TrackResults {...tracks} />
				<ImportHistory onRetry={managedImport.open} />
			</CollectionPageContainer>
			<ImportMusicDialog
				isOpen={managedImport.isOpen}
				onOpenChange={managedImport.handleOpenChange}
				onCommitted={managedImport.refresh}
				onCloseAutoFocus={managedImport.restoreFocus}
			/>
		</PageShell>
	);
}

function useTrackSearch() {
	const [search, setSearch] = useState("");
	const [lastTracks, setLastTracks] = useState<Track[]>([]);
	const debouncedSearch = useDebouncedValue(search.trim(), 250);
	const tracks = useQuery({
		queryKey: ["library", "tracks", debouncedSearch],
		queryFn: () =>
			apiClient.listTracks({ limit: 200, q: debouncedSearch || undefined }),
		placeholderData: (previous) => previous,
	});
	const sourceTracks = tracks.data?.items ?? lastTracks;
	const visibleTracks = useMemo(
		() => filterTracksByText(sourceTracks, search),
		[sourceTracks, search],
	);

	useEffect(() => {
		if (tracks.data?.items) {
			setLastTracks(tracks.data.items);
		}
	}, [tracks.data?.items]);
	return { search, setSearch, tracks, sourceTracks, visibleTracks };
}

const IMPORT_UNSUPPORTED_TITLE =
	"This Music Server does not support Managed Import. Update the Music Server to import music.";

function TracksHeader({
	search,
	onSearchChange,
	onImport,
	importCapability,
}: {
	search: string;
	onSearchChange: (search: string) => void;
	onImport: () => void;
	importCapability: ServerCapabilityState;
}) {
	// Gate on the advertised Server Capability (ADR 0006). The control stays
	// usable while the health response is unknown so a slow answer never hides
	// the Import Music action from a compatible Music Server.
	const importUnsupported = importCapability === "missing";
	return (
		<PageHeader
			title="Tracks"
			description="All tracks in your library"
			innerClassName={COLLECTION_PAGE_CONTAINER_CLASS}
			actions={
				<>
					<div className="relative w-full sm:max-w-md">
						<Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-caption" />
						<Input
							className="h-11 rounded-xl bg-[var(--player)] pl-10 text-sm"
							placeholder="Search tracks…"
							value={search}
							onChange={(event) => onSearchChange(event.target.value)}
						/>
					</div>
					<Button
						type="button"
						aria-label="Import Music"
						size="icon"
						className="size-10 rounded-xl"
						onClick={onImport}
						disabled={importUnsupported}
						title={importUnsupported ? IMPORT_UNSUPPORTED_TITLE : undefined}
					>
						<Plus className="size-5" />
					</Button>
				</>
			}
		/>
	);
}

function TrackResults({
	tracks,
	sourceTracks,
	visibleTracks,
}: ReturnType<typeof useTrackSearch>) {
	if (tracks.isLoading && sourceTracks.length === 0)
		return <p className="text-foreground text-sm">Loading tracks…</p>;
	if (tracks.isError && sourceTracks.length === 0)
		return <p className="text-destructive text-sm">Failed to load tracks</p>;
	if (visibleTracks.length === 0)
		return (
			<p className="text-foreground text-sm">No tracks match this search.</p>
		);
	return (
		<TrackList
			tracks={visibleTracks}
			showFavorite
			showMeta
			compact
			numbering="list"
		/>
	);
}

function useManagedImport() {
	const [isOpen, setIsOpen] = useState(false);
	const queryClient = useQueryClient();
	// The dialog has no DialogTrigger (it opens from the plus action or the
	// Import History retry), so remember the opener to restore focus on close.
	const returnFocus = useReturnFocus();
	async function refresh() {
		await Promise.all([
			queryClient.invalidateQueries({ queryKey: ["library", "tracks"] }),
			queryClient.invalidateQueries({
				queryKey: ["managed-import", "history"],
			}),
		]);
	}
	function handleOpenChange(nextIsOpen: boolean) {
		setIsOpen(nextIsOpen);
		if (!nextIsOpen)
			void queryClient.invalidateQueries({
				queryKey: ["managed-import", "history"],
			});
	}
	return {
		isOpen,
		open: () => {
			returnFocus.capture();
			setIsOpen(true);
		},
		handleOpenChange,
		refresh,
		restoreFocus: returnFocus.restore,
	};
}
