import type { Playlist, RadioStation, Track } from "@repo/api-client";
import { useNavigate } from "@tanstack/react-router";
import {
	Check,
	ChevronRight,
	Download,
	Info,
	MoreVertical,
	Plus,
	X,
} from "lucide-react";
import {
	type ReactNode,
	useCallback,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import { createPortal } from "react-dom";
import { cn } from "../lib/utils";
import { formatReplayGainAvailability } from "../playback/format-replay-gain";
import { usePlayback, usePlaylistLibrary } from "../playback/PlaybackProvider";
import { getQueuePanel } from "../widgets/layout-utils";
import { AlbumArt } from "./AlbumArt";
import { useLayout } from "./LayoutProvider";
import { PlaybackSignal } from "./PlaybackSignal";
import { PlaybackControls, VolumeAndQueueControls } from "./PlayerBarControls";

const RECENT_PLAYLISTS_KEY = "navidrome-recent-playlists";
const RECENT_PLAYLIST_LIMIT = 2;

function readRecentPlaylistIds(): string[] {
	if (typeof window === "undefined") return [];
	try {
		const raw = window.localStorage.getItem(RECENT_PLAYLISTS_KEY);
		if (!raw) return [];
		const parsed = JSON.parse(raw);
		return Array.isArray(parsed)
			? parsed.filter((id): id is string => typeof id === "string")
			: [];
	} catch {
		return [];
	}
}

function touchRecentPlaylist(playlistId: string) {
	if (typeof window === "undefined") return;
	const recent = readRecentPlaylistIds().filter((id) => id !== playlistId);
	recent.unshift(playlistId);
	window.localStorage.setItem(
		RECENT_PLAYLISTS_KEY,
		JSON.stringify(recent.slice(0, 10)),
	);
}

function formatSampleRate(hz?: number): string | null {
	if (!hz || hz <= 0) return null;
	if (hz % 1000 === 0) return `${hz / 1000} kHz`;
	return `${(hz / 1000).toFixed(1)} kHz`;
}

function formatQualityLabel(
	track: { bitrateKbps?: number; sampleRateHz?: number } | null,
): string {
	if (!track) return "Quality";
	const parts = [
		track.bitrateKbps && track.bitrateKbps > 0
			? `${track.bitrateKbps} kbps`
			: null,
		formatSampleRate(track.sampleRateHz),
	].filter(Boolean);
	return parts.length > 0 ? parts.join(" · ") : "Quality";
}

function formatRadioQualityLabel(station: RadioStation | null): string {
	if (!station) return "Quality";
	const parts = [
		station.codec ? station.codec.toUpperCase() : null,
		station.bitrate ? `${station.bitrate} kbps` : null,
	].filter(Boolean);
	return parts.length > 0 ? parts.join(" · ") : "High Quality";
}

function formatDuration(ms?: number): string | null {
	if (!ms || ms <= 0) return null;
	const total = Math.floor(ms / 1000);
	const minutes = Math.floor(total / 60);
	const seconds = total % 60;
	return `${minutes}m ${seconds}s`;
}

function formatBytes(bytes?: number): string | null {
	if (!bytes || bytes <= 0) return null;
	const mib = bytes / 1024 / 1024;
	return `${mib.toFixed(2)} MiB`;
}

function isLosslessFormat(format?: string): boolean {
	return ["flac", "alac", "wav", "aiff", "dsd"].includes(
		format?.toLowerCase() ?? "",
	);
}

type MenuPosition = {
	top: number;
	left: number;
};

export function PlayerBar({
	onPlaylistMutated,
}: {
	onPlaylistMutated?: () => void;
} = {}) {
	const navigate = useNavigate();
	const actionsButtonRef = useRef<HTMLButtonElement>(null);
	const playlistSubmenuCloseTimerRef = useRef<number | null>(null);
	const [actionsOpen, setActionsOpen] = useState(false);
	const [menuPosition, setMenuPosition] = useState<MenuPosition | null>(null);
	const [playlistSubmenuOpen, setPlaylistSubmenuOpen] = useState(false);
	const [infoOpen, setInfoOpen] = useState(false);
	const [playlists, setPlaylists] = useState<Playlist[]>([]);
	const [playlistsLoaded, setPlaylistsLoaded] = useState(false);
	const [memberPlaylistIds, setMemberPlaylistIds] = useState<Set<string>>(
		() => new Set(),
	);
	const [playlistQuery, setPlaylistQuery] = useState("");
	const [createPlaylistOpen, setCreatePlaylistOpen] = useState(false);
	const [newPlaylistName, setNewPlaylistName] = useState("");
	const { preferences, togglePanel } = useLayout();
	const queuePanelSide = getQueuePanel(preferences.layout.sidebarPosition);
	const {
		outputMode,
		outputDeviceIssue,
		currentTrack,
		currentRadioStation,
		radioNowPlaying,
		isPlaying,
		isReconnecting,
		currentTime,
		duration,
		volume,
		shuffleEnabled,
		repeatMode,
		playbackError,
		togglePlay,
		navigatePrevious,
		navigateNext,
		toggleShuffle,
		cycleRepeatMode,
		seek,
		setVolume,
		selectExclusiveOutput,
		fallbackToSystemOutput,
		enableAdaptiveSystemRate,
		getAlbumCoverUrl,
	} = usePlayback();
	const {
		listPlaylists,
		getPlaylist,
		createPlaylist,
		addPlaylistTrack,
		removePlaylistTrack,
	} = usePlaylistLibrary();

	const updateMenuPosition = useCallback(() => {
		const button = actionsButtonRef.current;
		if (!button) return;
		const rect = button.getBoundingClientRect();
		setMenuPosition({
			top: rect.top - 8,
			left: rect.left,
		});
	}, []);

	useEffect(() => {
		if (!actionsOpen) return;
		updateMenuPosition();
		const handlePointerDown = (event: MouseEvent) => {
			const target = event.target;
			if (!(target instanceof Node)) return;
			if (actionsButtonRef.current?.contains(target)) return;
			const menu = document.getElementById("player-track-actions-menu");
			if (menu?.contains(target)) return;
			setActionsOpen(false);
		};
		const handleKeyDown = (event: KeyboardEvent) => {
			if (event.key === "Escape") {
				setPlaylistSubmenuOpen(false);
				setActionsOpen(false);
			}
		};
		document.addEventListener("mousedown", handlePointerDown);
		document.addEventListener("keydown", handleKeyDown);
		window.addEventListener("resize", updateMenuPosition);
		window.addEventListener("scroll", updateMenuPosition, true);
		return () => {
			document.removeEventListener("mousedown", handlePointerDown);
			document.removeEventListener("keydown", handleKeyDown);
			window.removeEventListener("resize", updateMenuPosition);
			window.removeEventListener("scroll", updateMenuPosition, true);
		};
	}, [actionsOpen, updateMenuPosition]);

	const loadPlaylistMembership = useCallback(
		async (items: Playlist[], trackId: string) => {
			const details = await Promise.all(
				items.map((playlist) => getPlaylist(playlist.id)),
			);
			const memberIds = new Set<string>();
			for (const detail of details) {
				if (detail.tracks.some((entry) => entry.id === trackId)) {
					memberIds.add(detail.id);
				}
			}
			setMemberPlaylistIds(memberIds);
		},
		[getPlaylist],
	);

	const loadPlaylistsForSubmenu = useCallback(async () => {
		if (!currentTrack) return;
		const data = await listPlaylists();
		setPlaylists(data.items);
		await loadPlaylistMembership(data.items, currentTrack.id);
		setPlaylistsLoaded(true);
	}, [currentTrack, listPlaylists, loadPlaylistMembership]);

	useEffect(() => {
		if (!actionsOpen || !currentTrack) {
			setPlaylistSubmenuOpen(false);
			setPlaylistQuery("");
			setCreatePlaylistOpen(false);
			setNewPlaylistName("");
			setPlaylistsLoaded(false);
			return;
		}
		void loadPlaylistsForSubmenu();
	}, [actionsOpen, currentTrack, loadPlaylistsForSubmenu]);

	useEffect(() => {
		return () => {
			if (playlistSubmenuCloseTimerRef.current !== null) {
				window.clearTimeout(playlistSubmenuCloseTimerRef.current);
			}
		};
	}, []);

	const isRadioPlaying = Boolean(currentRadioStation);
	const hasPlayableSource = Boolean(currentTrack || currentRadioStation);
	const radioTitle =
		radioNowPlaying?.title ??
		radioNowPlaying?.raw ??
		currentRadioStation?.name ??
		null;
	const nowPlayingTitle =
		currentTrack?.title ?? radioTitle ?? "Nothing playing";
	const nowPlayingSubtitle = isReconnecting
		? "Reconnecting…"
		: (currentTrack?.artistName ??
			(isRadioPlaying
				? radioNowPlaying?.artist && radioNowPlaying.artist !== radioTitle
					? radioNowPlaying.artist
					: "Live radio"
				: null) ??
			"Select a track");
	const nowPlayingCaption = currentTrack?.albumTitle ?? null;
	const artworkUrl = currentTrack
		? getAlbumCoverUrl(currentTrack.albumId)
		: (currentRadioStation?.faviconUrl ?? null);

	const effectiveDuration =
		duration > 0
			? duration
			: currentTrack?.durationMs
				? currentTrack.durationMs / 1000
				: 0;

	const qualityLabel = isRadioPlaying
		? formatRadioQualityLabel(currentRadioStation)
		: formatQualityLabel(currentTrack);
	const hasActiveSource = currentTrack !== null || currentRadioStation !== null;
	const playbackAlert =
		playbackError?.message ?? outputDeviceIssue?.message ?? null;
	const sortedPlaylists = useMemo(
		() =>
			[...playlists].sort((a, b) => {
				if (a.isDefault !== b.isDefault) return a.isDefault ? -1 : 1;
				return a.name.localeCompare(b.name);
			}),
		[playlists],
	);

	const playlistSearchQuery = playlistQuery.trim().toLowerCase();

	const visiblePlaylists = useMemo(() => {
		if (playlistSearchQuery) {
			return sortedPlaylists.filter((playlist) =>
				playlist.name.toLowerCase().includes(playlistSearchQuery),
			);
		}
		const recentIds = readRecentPlaylistIds();
		const recent = recentIds
			.map((id) => sortedPlaylists.find((playlist) => playlist.id === id))
			.filter((playlist): playlist is Playlist => Boolean(playlist))
			.slice(0, RECENT_PLAYLIST_LIMIT);
		if (recent.length >= RECENT_PLAYLIST_LIMIT) return recent;
		const recentSet = new Set(recent.map((playlist) => playlist.id));
		for (const playlist of sortedPlaylists) {
			if (recent.length >= RECENT_PLAYLIST_LIMIT) break;
			if (!recentSet.has(playlist.id)) recent.push(playlist);
		}
		return recent.slice(0, RECENT_PLAYLIST_LIMIT);
	}, [playlistSearchQuery, sortedPlaylists]);

	const closeActionsMenu = () => {
		if (playlistSubmenuCloseTimerRef.current !== null) {
			window.clearTimeout(playlistSubmenuCloseTimerRef.current);
			playlistSubmenuCloseTimerRef.current = null;
		}
		setPlaylistSubmenuOpen(false);
		setActionsOpen(false);
	};

	const openPlaylistSubmenu = () => {
		if (playlistSubmenuCloseTimerRef.current !== null) {
			window.clearTimeout(playlistSubmenuCloseTimerRef.current);
			playlistSubmenuCloseTimerRef.current = null;
		}
		setPlaylistSubmenuOpen(true);
		if (!playlistsLoaded) void loadPlaylistsForSubmenu();
	};

	const closePlaylistSubmenu = () => {
		if (playlistSubmenuCloseTimerRef.current !== null) {
			window.clearTimeout(playlistSubmenuCloseTimerRef.current);
			playlistSubmenuCloseTimerRef.current = null;
		}
		setPlaylistSubmenuOpen(false);
		setPlaylistQuery("");
		setCreatePlaylistOpen(false);
		setNewPlaylistName("");
	};

	const schedulePlaylistSubmenuClose = () => {
		if (playlistSubmenuCloseTimerRef.current !== null) {
			window.clearTimeout(playlistSubmenuCloseTimerRef.current);
		}
		playlistSubmenuCloseTimerRef.current = window.setTimeout(() => {
			closePlaylistSubmenu();
		}, 200);
	};

	const openInfoModal = () => {
		closeActionsMenu();
		setInfoOpen(true);
	};

	const handleGoToAlbum = () => {
		if (!currentTrack) return;
		closeActionsMenu();
		void navigate({
			to: "/library/$albumId",
			params: { albumId: currentTrack.albumId },
		});
	};

	const handleGoToArtist = () => {
		if (!currentTrack) return;
		closeActionsMenu();
		void navigate({
			to: "/library/artists",
			search: { q: currentTrack.artistName },
		});
	};

	const notifyPlaylistMutated = () => {
		onPlaylistMutated?.();
	};

	const refreshPlaylists = async () => {
		const data = await listPlaylists();
		setPlaylists(data.items);
		if (currentTrack) {
			await loadPlaylistMembership(data.items, currentTrack.id);
		}
		notifyPlaylistMutated();
	};

	const handleTogglePlaylist = async (playlistId: string) => {
		if (!currentTrack) return;
		if (memberPlaylistIds.has(playlistId)) {
			await removePlaylistTrack(playlistId, currentTrack.id);
			setMemberPlaylistIds((current) => {
				const next = new Set(current);
				next.delete(playlistId);
				return next;
			});
		} else {
			await addPlaylistTrack(playlistId, currentTrack.id);
			setMemberPlaylistIds((current) => new Set(current).add(playlistId));
			touchRecentPlaylist(playlistId);
		}
		const data = await listPlaylists();
		setPlaylists(data.items);
		notifyPlaylistMutated();
	};

	const handleCreatePlaylist = async () => {
		if (!currentTrack || !newPlaylistName.trim()) return;
		const playlist = await createPlaylist(newPlaylistName.trim());
		await addPlaylistTrack(playlist.id, currentTrack.id);
		touchRecentPlaylist(playlist.id);
		setNewPlaylistName("");
		setCreatePlaylistOpen(false);
		await refreshPlaylists();
	};

	return (
		<footer className="relative h-[72px] border-[var(--shell-subtle-border)] border-t bg-player px-6 pt-px text-player-foreground shadow-[0px_-10px_40px_0px_rgba(0,0,0,0.3)] backdrop-blur-[12px]">
			{playbackAlert ? (
				<p
					role="alert"
					className="absolute bottom-full left-1/2 mb-2 -translate-x-1/2 rounded-md border border-destructive/40 bg-popover px-3 py-2 text-destructive text-sm shadow-lg"
				>
					{playbackAlert}
				</p>
			) : null}
			<div className="flex h-full w-full min-w-0 items-center justify-between gap-6">
				<section
					aria-label="Now playing"
					className="flex min-w-[200px] flex-[1_0_0] items-center gap-4 justify-self-start"
				>
					<AlbumArt
						coverUrl={artworkUrl}
						title={nowPlayingTitle}
						className="size-12 shrink-0 rounded-[2px] border border-[var(--shell-subtle-border)] bg-[var(--player-artwork)] text-sm"
					/>
					<div className="min-w-0 overflow-hidden">
						<div className="flex max-w-full min-w-0 items-center">
							<p
								className="min-w-0 truncate font-medium text-[var(--player-title)] text-sm"
								title={nowPlayingTitle}
							>
								{nowPlayingTitle}
							</p>
							<button
								ref={actionsButtonRef}
								type="button"
								className={cn(
									"ml-2 inline-flex size-6 shrink-0 items-center justify-center rounded text-player-foreground hover:text-[var(--player-control-primary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--player-control-primary)]/40 disabled:opacity-40",
									actionsOpen && "text-[var(--player-control-primary)]",
								)}
								aria-label="Track actions"
								aria-expanded={actionsOpen}
								disabled={!currentTrack}
								onClick={() => {
									setActionsOpen((open) => {
										const next = !open;
										if (next) updateMenuPosition();
										return next;
									});
								}}
							>
								<MoreVertical className="size-3.5" />
							</button>
						</div>
						<p
							className="truncate text-player-foreground text-[11px]"
							title={nowPlayingSubtitle}
							role={isReconnecting ? "status" : undefined}
							aria-live={isReconnecting ? "polite" : undefined}
						>
							{nowPlayingSubtitle}
						</p>
						{nowPlayingCaption ? (
							<p
								className="hidden truncate text-caption text-xs sm:block"
								title={nowPlayingCaption}
							>
								{nowPlayingCaption}
							</p>
						) : null}
						{actionsOpen && currentTrack && menuPosition ? (
							<Portal>
								<div
									id="player-track-actions-menu"
									className="fixed z-50 min-w-44 -translate-y-full rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-lg"
									style={{
										top: menuPosition.top,
										left: menuPosition.left,
									}}
									role="menu"
								>
									<AddToPlaylistMenuItem
										open={playlistSubmenuOpen}
										onOpen={openPlaylistSubmenu}
										onClose={schedulePlaylistSubmenuClose}
										query={playlistQuery}
										onQueryChange={setPlaylistQuery}
										createOpen={createPlaylistOpen}
										onCreateOpen={() => setCreatePlaylistOpen(true)}
										newPlaylistName={newPlaylistName}
										onNewPlaylistNameChange={setNewPlaylistName}
										playlists={visiblePlaylists}
										memberPlaylistIds={memberPlaylistIds}
										isSearching={Boolean(playlistSearchQuery)}
										onToggle={(playlistId) =>
											void handleTogglePlaylist(playlistId)
										}
										onCreate={() => void handleCreatePlaylist()}
									/>
									<MenuButton onClick={handleGoToAlbum}>Go to album</MenuButton>
									<MenuButton onClick={handleGoToArtist}>
										Go to artist
									</MenuButton>
									<MenuButton disabled>
										<Download className="size-3.5" />
										Download
									</MenuButton>
									<MenuButton onClick={openInfoModal}>
										<Info className="size-3.5" />
										Details
									</MenuButton>
								</div>
							</Portal>
						) : null}
					</div>
				</section>

				<PlaybackControls
					isRadioPlaying={isRadioPlaying}
					isPlaying={isPlaying}
					hasPlayableSource={hasPlayableSource}
					hasCurrentTrack={Boolean(currentTrack)}
					currentTime={currentTime}
					effectiveDuration={effectiveDuration}
					shuffleEnabled={shuffleEnabled}
					repeatMode={repeatMode}
					onTogglePlay={togglePlay}
					onToggleShuffle={toggleShuffle}
					onCycleRepeatMode={cycleRepeatMode}
					onPrevious={navigatePrevious}
					onNext={navigateNext}
					onSeek={seek}
				/>

				<VolumeAndQueueControls
					qualityLabel={qualityLabel}
					isLossless={isLosslessFormat(currentTrack?.format)}
					volume={volume}
					signalControl={
						hasActiveSource && outputMode ? (
							<PlaybackSignal
								outputMode={outputMode}
								outputControls={{
									selectNormalOutput: fallbackToSystemOutput,
									selectExclusiveOutput,
									enableAdaptiveSystemRate,
								}}
							/>
						) : undefined
					}
					onToggleQueue={() => togglePanel(queuePanelSide)}
					onVolumeChange={setVolume}
				/>
			</div>
			{infoOpen && currentTrack ? (
				<TrackInfoDialog
					track={currentTrack}
					onClose={() => setInfoOpen(false)}
				/>
			) : null}
		</footer>
	);
}

function Portal({ children }: { children: ReactNode }) {
	if (typeof document === "undefined") return null;
	return createPortal(children, document.body);
}

function AddToPlaylistMenuItem({
	open,
	onOpen,
	onClose,
	query,
	onQueryChange,
	createOpen,
	onCreateOpen,
	newPlaylistName,
	onNewPlaylistNameChange,
	playlists,
	memberPlaylistIds,
	isSearching,
	onToggle,
	onCreate,
}: {
	open: boolean;
	onOpen: () => void;
	onClose: () => void;
	query: string;
	onQueryChange: (value: string) => void;
	createOpen: boolean;
	onCreateOpen: () => void;
	newPlaylistName: string;
	onNewPlaylistNameChange: (value: string) => void;
	playlists: {
		id: string;
		name: string;
		isDefault: boolean;
		trackCount: number;
	}[];
	memberPlaylistIds: Set<string>;
	isSearching: boolean;
	onToggle: (playlistId: string) => void;
	onCreate: () => void;
}) {
	return (
		<div
			className="relative"
			onMouseEnter={onOpen}
			onMouseLeave={onClose}
			role="none"
		>
			<div
				className={cn(
					"flex w-full items-center justify-between gap-2 rounded-sm px-2 py-1.5 text-left text-xs",
					open && "bg-muted text-heading",
				)}
				role="menuitem"
				aria-haspopup="menu"
				aria-expanded={open}
				tabIndex={0}
				onFocus={onOpen}
				onBlur={onClose}
			>
				<span className="flex items-center gap-2">Add to playlist</span>
				<ChevronRight className="size-3.5 text-caption" />
			</div>
			{open ? (
				<div
					className="absolute top-0 left-full z-50 ml-1 w-56 rounded-md border border-border bg-popover p-2 text-popover-foreground shadow-lg"
					role="menu"
					aria-label="Add to playlist"
					onMouseEnter={onOpen}
					onMouseLeave={onClose}
				>
					<input
						type="search"
						placeholder="Search playlists"
						value={query}
						onChange={(event) => onQueryChange(event.target.value)}
						className="mb-2 h-8 w-full rounded-md border border-border bg-background px-2.5 text-xs outline-none focus:ring-2 focus:ring-primary/40"
						onClick={(event) => event.stopPropagation()}
					/>
					{createOpen ? (
						<div className="mb-2 flex gap-1.5">
							<label className="sr-only" htmlFor="player-new-playlist-name">
								New playlist name
							</label>
							<input
								id="player-new-playlist-name"
								value={newPlaylistName}
								onChange={(event) =>
									onNewPlaylistNameChange(event.target.value)
								}
								className="h-8 min-w-0 flex-1 rounded-md border border-border bg-background px-2.5 text-xs outline-none focus:ring-2 focus:ring-primary/40"
								placeholder="Playlist name"
							/>
							<button
								type="button"
								className="inline-flex h-8 items-center rounded-md bg-primary px-2.5 font-medium text-primary-foreground text-xs disabled:opacity-50"
								disabled={!newPlaylistName.trim()}
								onClick={onCreate}
							>
								Create
							</button>
						</div>
					) : (
						<button
							type="button"
							className="mb-2 flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs hover:bg-muted"
							role="menuitem"
							onClick={onCreateOpen}
						>
							<span className="inline-flex size-5 items-center justify-center rounded-full border border-border">
								<Plus className="size-3" />
							</span>
							Create new playlist
						</button>
					)}
					{!isSearching && playlists.length > 0 ? (
						<p className="mb-1 px-2 font-medium text-[0.625rem] text-caption uppercase tracking-wide">
							Recent
						</p>
					) : null}
					<div className="max-h-40 overflow-auto">
						{playlists.length === 0 ? (
							<p className="px-2 py-1.5 text-caption text-xs">
								{isSearching ? "No playlists found" : "No playlists yet"}
							</p>
						) : (
							playlists.map((playlist) => {
								const isMember = memberPlaylistIds.has(playlist.id);
								return (
									<button
										key={playlist.id}
										type="button"
										className="flex w-full items-center justify-between gap-2 rounded-sm px-2 py-1.5 text-left text-xs hover:bg-muted"
										role="menuitem"
										aria-label={
											isMember
												? `Remove from ${playlist.name}`
												: `Add to ${playlist.name}`
										}
										onClick={() => onToggle(playlist.id)}
									>
										<span className="min-w-0 truncate">{playlist.name}</span>
										{isMember ? (
											<Check className="size-3.5 shrink-0 text-heading" />
										) : null}
									</button>
								);
							})
						)}
					</div>
				</div>
			) : null}
		</div>
	);
}

function MenuButton({
	children,
	disabled = false,
	onClick,
}: {
	children: ReactNode;
	disabled?: boolean;
	onClick?: () => void;
}) {
	return (
		<button
			type="button"
			className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
			role="menuitem"
			disabled={disabled}
			onClick={onClick}
		>
			{children}
		</button>
	);
}

function TrackInfoDialog({
	track,
	onClose,
}: {
	track: Track;
	onClose: () => void;
}) {
	const rows = [
		["Title", track.title],
		["Artist", track.artistName],
		["Album", track.albumTitle],
		["Track", track.trackNo?.toString()],
		["Duration", formatDuration(track.durationMs)],
		["Codec", track.format],
		["Bitrate", track.bitrateKbps ? `${track.bitrateKbps} kbps` : null],
		["Sample rate", formatSampleRate(track.sampleRateHz)],
		["Bit depth", track.bitDepth ? `${track.bitDepth}-bit` : null],
		[
			"Track ReplayGain",
			formatReplayGainAvailability(
				track.replayGain?.trackGainDb,
				track.replayGain?.trackPeak,
			),
		],
		[
			"Album ReplayGain",
			formatReplayGainAvailability(
				track.replayGain?.albumGainDb,
				track.replayGain?.albumPeak,
			),
		],
		["Genre", track.genre],
		["Size", formatBytes(track.sizeBytes)],
		["Id", track.id],
	].filter((row): row is [string, string] => Boolean(row[1]));

	return (
		<Portal>
			<div className="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4">
				<div
					role="dialog"
					aria-modal="true"
					aria-label={track.title}
					className="max-h-[80vh] w-full max-w-2xl overflow-hidden rounded-lg border border-border bg-popover text-popover-foreground shadow-xl"
				>
					<div className="flex items-center justify-between gap-3 border-border border-b p-4">
						<h2 className="truncate font-semibold text-heading text-xl">
							{track.title}
						</h2>
						<button
							type="button"
							className="inline-flex size-8 items-center justify-center rounded-full hover:bg-muted"
							aria-label="Close"
							onClick={onClose}
						>
							<X className="size-4" />
						</button>
					</div>
					<div className="max-h-[65vh] overflow-auto p-4">
						{rows.map(([label, value]) => (
							<div
								key={label}
								className="grid grid-cols-[8rem_minmax(0,1fr)] gap-3 border-border border-b py-2 text-sm"
							>
								<span className="text-caption">{label}</span>
								<span className="min-w-0 break-words font-medium text-foreground">
									{value}
								</span>
							</div>
						))}
					</div>
				</div>
			</div>
		</Portal>
	);
}
