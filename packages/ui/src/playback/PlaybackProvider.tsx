import type {
	Playlist,
	PlaylistDetail,
	PlaylistList,
	QueueItem,
	RadioNowPlaying,
	RadioSearchResult,
	RadioStation,
	Track,
} from "@repo/api-client";
import {
	createContext,
	type ReactNode,
	useCallback,
	useContext,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import type {
	PlaybackEngine,
	PlaybackError,
	PlaybackSessionState,
	PlaybackSource,
	RepeatMode,
} from "./PlaybackEngine";
import type {
	EqualizerPreset,
	OutputDevice,
	OutputDeviceIssue,
	OutputMode,
	ProcessingProfile,
	ProcessingState,
	ReplayGainMode,
} from "./processing";
import type { PlaybackTelemetry } from "./telemetry";
import {
	type PlaybackQueueApi,
	useSynchronizedQueue,
} from "./use-synchronized-queue";

export type { RepeatMode } from "./PlaybackEngine";
export type { PlaybackQueueApi } from "./use-synchronized-queue";

export type PlaybackAssetApi = {
	getStreamUrl: (trackId: string) => string;
	getAlbumCoverUrl: (albumId: string) => string;
};

export type PlaylistLibraryApi = {
	listPlaylists: () => Promise<PlaylistList>;
	getPlaylist: (playlistId: string) => Promise<PlaylistDetail>;
	createPlaylist: (name: string) => Promise<Playlist>;
	addPlaylistTrack: (
		playlistId: string,
		trackId: string,
	) => Promise<PlaylistDetail>;
	removePlaylistTrack: (
		playlistId: string,
		trackId: string,
	) => Promise<PlaylistDetail>;
};

export type RadioPlaybackApi = {
	getRadioStationStreamUrl: (stationId: string) => string;
	getRadioCatalogPreviewStreamUrl: (stationUuid: string) => string;
	getRadioNowPlaying: (stationId: string) => Promise<RadioNowPlaying>;
};

export type PlaybackApi = PlaybackQueueApi &
	PlaybackAssetApi &
	PlaylistLibraryApi &
	RadioPlaybackApi;

type PlaybackContextValue = {
	queue: QueueItem[];
	playbackSource: PlaybackSource | null;
	outputMode: OutputMode | null;
	availableOutputDevices: OutputDevice[];
	selectedOutputDevice: OutputDevice | null;
	outputDeviceIssue: OutputDeviceIssue | null;
	currentTrack: Track | null;
	currentRadioStation: RadioStation | null;
	radioNowPlaying: RadioNowPlaying | null;
	isPlaying: boolean;
	isReconnecting: boolean;
	currentTime: number;
	duration: number;
	volume: number;
	shuffleEnabled: boolean;
	repeatMode: RepeatMode;
	playbackError: PlaybackError | null;
	processingState: ProcessingState | null;
	playbackTelemetry: PlaybackTelemetry | null;
	queueConflict: string | null;
	playTrack: (trackId: string, queueTrackIds?: string[]) => Promise<void>;
	playRadioStation: (station: RadioStation) => Promise<void>;
	playRadioCatalogPreview: (result: RadioSearchResult) => Promise<void>;
	queueTracks: (trackIds: string[]) => Promise<void>;
	playQueueIndex: (index: number) => Promise<void>;
	playNext: (trackId: string) => Promise<void>;
	navigatePrevious: () => void;
	navigateNext: () => void;
	togglePlay: () => void;
	toggleShuffle: () => void;
	cycleRepeatMode: () => void;
	seek: (seconds: number) => void;
	setVolume: (value: number) => void;
	setProcessingProfile: (profile: ProcessingProfile) => void;
	setReplayGainMode: (mode: ReplayGainMode) => void;
	setEqualizerPreset: (preset: Exclude<EqualizerPreset, "custom">) => void;
	setEqualizerGain: (index: number, value: number) => void;
	refreshOutputDevices: () => void;
	selectDirectAlsaOutput: (deviceId: string) => void;
	selectExclusiveOutput: () => void;
	fallbackToSystemOutput: () => void;
	enableAdaptiveSystemRate: () => void;
	removeFromQueue: (itemId: string) => Promise<void>;
	reorderQueue: (itemIds: string[]) => Promise<void>;
	clearQueue: () => Promise<void>;
	refreshQueue: () => Promise<void>;
	stopPlayback: () => void;
	getAlbumCoverUrl: (albumId: string) => string;
};

const PlaybackContext = createContext<PlaybackContextValue | null>(null);
const PlaylistLibraryContext = createContext<PlaylistLibraryApi | null>(null);

export function PlaybackProvider({
	children,
	api,
	engine,
}: {
	children: ReactNode;
	api: PlaybackApi;
	engine: PlaybackEngine;
}) {
	const apiRef = useRef(api);
	const {
		queue,
		queueRef,
		queueConflict,
		refreshQueue,
		appendQueueItem,
		replaceQueue,
		removeFromQueue,
		reorderQueue,
	} = useSynchronizedQueue(api);
	const [session, setSession] = useState<PlaybackSessionState>(() =>
		engine.getState(),
	);
	const currentQueueItemIdRef = useRef<string | null>(null);
	const currentQueueIndexRef = useRef<number | null>(null);
	const repeatCancellationRequestedModeRef = useRef<RepeatMode | null>(null);
	const [radioNowPlaying, setRadioNowPlaying] =
		useState<RadioNowPlaying | null>(null);
	apiRef.current = api;

	useEffect(() => {
		setSession(engine.getState());
		return engine.subscribe(setSession);
	}, [engine]);

	const currentTrack =
		session.source?.type === "track" ? session.source.track : null;
	const currentRadioStation = useMemo(
		() => getCurrentRadioStation(session),
		[session.source],
	);
	const radioNowPlayingStationId =
		session.source?.type === "radio-station" ? session.source.station.id : null;

	const refreshRadioNowPlaying = useCallback(async () => {
		if (!radioNowPlayingStationId) return;
		try {
			const data = await apiRef.current.getRadioNowPlaying(
				radioNowPlayingStationId,
			);
			setRadioNowPlaying(data);
		} catch (error) {
			console.warn("Failed to refresh radio now playing", {
				stationId: radioNowPlayingStationId,
				error,
			});
		}
	}, [radioNowPlayingStationId]);

	useEffect(() => {
		if (!radioNowPlayingStationId) return undefined;
		void refreshRadioNowPlaying();
		const intervalId = window.setInterval(
			() => void refreshRadioNowPlaying(),
			30000,
		);
		return () => window.clearInterval(intervalId);
	}, [radioNowPlayingStationId, refreshRadioNowPlaying]);

	const playQueueItemInternal = useCallback(
		async (item: QueueItem, queueOverride?: QueueItem[]) => {
			const activeQueue = queueOverride ?? queueRef.current;
			const itemIndex = activeQueue.findIndex((entry) => entry.id === item.id);
			if (itemIndex < 0) return;
			currentQueueItemIdRef.current = item.id;
			currentQueueIndexRef.current = itemIndex;
			setRadioNowPlaying(null);
			try {
				await engine.syncQueueContext?.(
					queuePlaybackSources(activeQueue, apiRef.current),
					itemIndex,
				);
				await engine.play({
					type: "track",
					track: item.track,
					playbackUrl: apiRef.current.getStreamUrl(item.trackId),
					queueItemId: item.id,
				});
			} catch {
				// PlaybackEngine exposes the error through observable session state.
			}
		},
		[engine],
	);

	useEffect(() => {
		if (engine.syncQueueContext || !engine.subscribeNavigation)
			return undefined;
		return engine.subscribeNavigation((direction) => {
			const currentIndex = queueRef.current.findIndex(
				(item) => item.id === currentQueueItemIdRef.current,
			);
			if (currentIndex < 0) return;
			const offset = direction === "previous" ? -1 : 1;
			const item = queueRef.current[currentIndex + offset];
			if (item) void playQueueItemInternal(item);
		});
	}, [engine, playQueueItemInternal]);

	useEffect(() => {
		if (!engine.syncQueueContext) return;
		const currentIndex = queue.findIndex(
			(item) => item.id === currentQueueItemIdRef.current,
		);
		void engine
			.syncQueueContext(
				queuePlaybackSources(queue, apiRef.current),
				currentIndex >= 0 ? currentIndex : null,
			)
			.catch((error) => {
				console.warn("Failed to sync native playback Queue context", { error });
			});
	}, [engine, queue]);

	const playTrackInternal = useCallback(
		async (trackId: string, queueOverride?: QueueItem[]) => {
			const activeQueue = queueOverride ?? queueRef.current;
			const item = activeQueue.find(
				(entry) => entry.track.id === trackId || entry.trackId === trackId,
			);
			if (item) await playQueueItemInternal(item, activeQueue);
		},
		[playQueueItemInternal],
	);

	useEffect(() => {
		if (session.source?.type !== "track") return;
		if (session.source.queueItemId) {
			currentQueueItemIdRef.current = session.source.queueItemId;
		}
		const itemIndex = queue.findIndex(
			(item) => item.id === currentQueueItemIdRef.current,
		);
		if (itemIndex < 0) return;
		currentQueueIndexRef.current = itemIndex;
	}, [queue, session.source]);

	useEffect(() => {
		if (session.repeatMode === "off") {
			repeatCancellationRequestedModeRef.current = null;
			return;
		}
		if (queue.length > 0 || session.source?.type !== "track") {
			repeatCancellationRequestedModeRef.current = null;
			return;
		}
		if (repeatCancellationRequestedModeRef.current === session.repeatMode)
			return;
		repeatCancellationRequestedModeRef.current = session.repeatMode;
		engine.cycleRepeatMode();
	}, [engine, queue.length, session.repeatMode, session.source]);

	useEffect(() => {
		if (engine.syncQueueContext) return;
		if (session.status !== "ended" || session.source?.type !== "track") return;
		const index = queueRef.current.findIndex(
			(item) => item.id === currentQueueItemIdRef.current,
		);
		const nextIndex =
			index >= 0 ? index + 1 : (currentQueueIndexRef.current ?? 0);
		const next = queueRef.current[nextIndex];
		if (next) {
			void playQueueItemInternal(next);
			return;
		}
		engine.stop();
	}, [engine, playQueueItemInternal, session]);

	const playTrack = useCallback(
		async (trackId: string, queueTrackIds?: string[]) => {
			let nextQueue = queueRef.current;
			if (queueTrackIds) {
				const data = await replaceQueue(queueTrackIds);
				if (!data) return;
				nextQueue = data.items;
			}
			if (!nextQueue.some((item) => item.track.id === trackId)) {
				const data = await appendQueueItem(trackId);
				nextQueue = data.items;
			}
			await playTrackInternal(trackId, nextQueue);
		},
		[appendQueueItem, playTrackInternal, replaceQueue],
	);

	const playRadioStation = useCallback(
		async (station: RadioStation) => {
			setRadioNowPlaying(station.lastNowPlaying ?? null);
			try {
				await engine.play({
					type: "radio-station",
					station,
					playbackUrl: apiRef.current.getRadioStationStreamUrl(station.id),
					sourceUrl: station.streamUrl,
				});
			} catch (error) {
				console.warn("Failed to start radio station playback", {
					stationId: station.id,
					error,
				});
			}
		},
		[engine],
	);

	const playRadioCatalogPreview = useCallback(
		async (result: RadioSearchResult) => {
			setRadioNowPlaying(null);
			try {
				await engine.play({
					type: "catalog-preview",
					result,
					playbackUrl: apiRef.current.getRadioCatalogPreviewStreamUrl(
						result.stationUuid,
					),
					sourceUrl: result.streamUrl,
				});
			} catch (error) {
				console.warn("Failed to start radio catalog preview", {
					stationUuid: result.stationUuid,
					error,
				});
				throw error;
			}
		},
		[engine],
	);

	const queueTracks = useCallback(
		async (trackIds: string[]) => {
			for (const trackId of trackIds) {
				await appendQueueItem(trackId);
			}
		},
		[appendQueueItem],
	);

	const playQueueIndex = useCallback(
		async (index: number) => {
			const item = queueRef.current[index];
			if (item) await playQueueItemInternal(item);
		},
		[playQueueItemInternal],
	);

	const playNext = useCallback(
		async (trackId: string) => {
			const trackIds = queueRef.current
				.map((item) => item.track.id)
				.filter((id) => id !== trackId);
			const currentIndex = currentTrack
				? queueRef.current.findIndex(
						(item) => item.track.id === currentTrack.id,
					)
				: -1;
			trackIds.splice(
				currentIndex >= 0 ? currentIndex + 1 : trackIds.length,
				0,
				trackId,
			);
			await replaceQueue(trackIds);
		},
		[currentTrack, replaceQueue],
	);

	const clearQueue = useCallback(async () => {
		await replaceQueue([]);
	}, [replaceQueue]);

	const value = useMemo<PlaybackContextValue>(
		() => ({
			queue,
			playbackSource: session.source,
			outputMode: session.outputMode,
			availableOutputDevices: session.availableOutputDevices,
			selectedOutputDevice: session.selectedOutputDevice,
			outputDeviceIssue: session.outputDeviceIssue,
			currentTrack,
			currentRadioStation,
			radioNowPlaying,
			isPlaying:
				session.status === "playing" || session.status === "reconnecting",
			isReconnecting: session.status === "reconnecting",
			currentTime: session.currentTime,
			duration: session.duration,
			volume: session.volume,
			shuffleEnabled: session.shuffleEnabled,
			repeatMode: session.repeatMode,
			playbackError: session.error,
			processingState: session.processing ?? null,
			playbackTelemetry: session.telemetry ?? null,
			queueConflict,
			playTrack,
			playRadioStation,
			playRadioCatalogPreview,
			queueTracks,
			playQueueIndex,
			playNext,
			navigatePrevious: () => engine.previous(),
			navigateNext: () => engine.next(),
			togglePlay: () => engine.togglePlay(),
			toggleShuffle: () => engine.toggleShuffle(),
			cycleRepeatMode: () => engine.cycleRepeatMode(),
			seek: (seconds) => engine.seek(seconds),
			setVolume: (value) => engine.setVolume(value),
			setProcessingProfile: (profile) => engine.setProcessingProfile?.(profile),
			setReplayGainMode: (mode) => engine.setReplayGainMode?.(mode),
			setEqualizerPreset: (preset) => engine.setEqualizerPreset?.(preset),
			setEqualizerGain: (index, value) =>
				engine.setEqualizerGain?.(index, value),
			refreshOutputDevices: () => engine.refreshOutputDevices?.(),
			selectDirectAlsaOutput: (deviceId) =>
				engine.selectDirectAlsaOutput?.(deviceId),
			selectExclusiveOutput: () => engine.selectExclusiveOutput?.(),
			fallbackToSystemOutput: () => engine.fallbackToSystemOutput?.(),
			enableAdaptiveSystemRate: () => engine.enableAdaptiveSystemRate?.(),
			removeFromQueue,
			reorderQueue,
			clearQueue,
			refreshQueue,
			stopPlayback: () => engine.stop(),
			getAlbumCoverUrl: (albumId) => apiRef.current.getAlbumCoverUrl(albumId),
		}),
		[
			queue,
			queueConflict,
			currentTrack,
			currentRadioStation,
			radioNowPlaying,
			session,
			playTrack,
			playRadioStation,
			playRadioCatalogPreview,
			queueTracks,
			playQueueIndex,
			playNext,
			engine,
			removeFromQueue,
			reorderQueue,
			clearQueue,
			refreshQueue,
		],
	);

	const playlistLibrary = useMemo<PlaylistLibraryApi>(
		() => ({
			listPlaylists: () => apiRef.current.listPlaylists(),
			getPlaylist: (playlistId) => apiRef.current.getPlaylist(playlistId),
			createPlaylist: (name) => apiRef.current.createPlaylist(name),
			addPlaylistTrack: (playlistId, trackId) =>
				apiRef.current.addPlaylistTrack(playlistId, trackId),
			removePlaylistTrack: (playlistId, trackId) =>
				apiRef.current.removePlaylistTrack(playlistId, trackId),
		}),
		[],
	);

	return (
		<PlaybackContext.Provider value={value}>
			<PlaylistLibraryContext.Provider value={playlistLibrary}>
				{children}
			</PlaylistLibraryContext.Provider>
		</PlaybackContext.Provider>
	);
}

function queuePlaybackSources(
	queue: QueueItem[],
	api: PlaybackAssetApi,
): PlaybackSource[] {
	return queue.map((item) => ({
		type: "track",
		track: item.track,
		playbackUrl: api.getStreamUrl(item.trackId),
		queueItemId: item.id,
	}));
}

export function usePlayback() {
	const context = useContext(PlaybackContext);
	if (!context)
		throw new Error("usePlayback must be used within PlaybackProvider");
	return context;
}

export function usePlaylistLibrary() {
	const context = useContext(PlaylistLibraryContext);
	if (!context)
		throw new Error("usePlaylistLibrary must be used within PlaybackProvider");
	return context;
}

function getCurrentRadioStation(
	session: PlaybackSessionState,
): RadioStation | null {
	if (session.source?.type === "radio-station") return session.source.station;
	if (session.source?.type !== "catalog-preview") return null;
	const result = session.source.result;
	return {
		id: `preview:${result.stationUuid}`,
		name: result.name,
		streamUrl: result.streamUrl,
		homepageUrl: result.homepageUrl,
		faviconUrl: result.faviconUrl,
		country: result.country,
		language: result.language,
		tags: result.tags,
		codec: result.codec,
		bitrate: result.bitrate,
		source: "radio-browser",
		externalId: result.stationUuid,
		isFavorite: false,
		position: -1,
	};
}
