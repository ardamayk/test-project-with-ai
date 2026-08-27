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
	RepeatMode,
} from "./PlaybackEngine";
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
	currentTrack: Track | null;
	currentRadioStation: RadioStation | null;
	radioNowPlaying: RadioNowPlaying | null;
	isPlaying: boolean;
	currentTime: number;
	duration: number;
	volume: number;
	shuffleEnabled: boolean;
	repeatMode: RepeatMode;
	playbackError: PlaybackError | null;
	queueConflict: string | null;
	playTrack: (trackId: string, queueTrackIds?: string[]) => Promise<void>;
	playRadioStation: (station: RadioStation) => Promise<void>;
	playRadioCatalogPreview: (result: RadioSearchResult) => Promise<void>;
	queueTracks: (trackIds: string[]) => Promise<void>;
	playQueueIndex: (index: number) => Promise<void>;
	playNext: (trackId: string) => Promise<void>;
	togglePlay: () => void;
	toggleShuffle: () => void;
	cycleRepeatMode: () => void;
	seek: (seconds: number) => void;
	setVolume: (value: number) => void;
	removeFromQueue: (itemId: string) => Promise<void>;
	reorderQueue: (itemIds: string[]) => Promise<void>;
	clearQueue: () => Promise<void>;
	refreshQueue: () => Promise<void>;
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

	const refreshRadioNowPlaying = useCallback(async () => {
		if (!currentRadioStation) return;
		try {
			const data = await apiRef.current.getRadioNowPlaying(
				currentRadioStation.id,
			);
			setRadioNowPlaying(data);
		} catch (error) {
			console.warn("Failed to refresh radio now playing", {
				stationId: currentRadioStation.id,
				error,
			});
		}
	}, [currentRadioStation]);

	useEffect(() => {
		if (!currentRadioStation) return undefined;
		void refreshRadioNowPlaying();
		const intervalId = window.setInterval(
			() => void refreshRadioNowPlaying(),
			30000,
		);
		return () => window.clearInterval(intervalId);
	}, [currentRadioStation, refreshRadioNowPlaying]);

	const playTrackInternal = useCallback(
		async (trackId: string, queueOverride?: QueueItem[]) => {
			const item = (queueOverride ?? queueRef.current).find(
				(entry) => entry.track.id === trackId || entry.trackId === trackId,
			);
			if (!item) return;
			setRadioNowPlaying(null);
			try {
				await engine.play({
					type: "track",
					track: item.track,
					playbackUrl: apiRef.current.getStreamUrl(trackId),
				});
			} catch {
				// PlaybackEngine exposes the error through observable session state.
			}
		},
		[engine],
	);

	useEffect(() => {
		if (session.status !== "ended" || session.source?.type !== "track") return;
		const endedTrackId = session.source.track.id;
		const index = queueRef.current.findIndex(
			(item) => item.track.id === endedTrackId,
		);
		const next = queueRef.current[index + 1];
		if (next) void playTrackInternal(next.track.id);
	}, [playTrackInternal, session]);

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
			if (item) await playTrackInternal(item.track.id);
		},
		[playTrackInternal],
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
		const data = await replaceQueue([]);
		if (!data) return;
		setRadioNowPlaying(null);
		engine.stop();
	}, [engine, replaceQueue]);

	const value = useMemo<PlaybackContextValue>(
		() => ({
			queue,
			currentTrack,
			currentRadioStation,
			radioNowPlaying,
			isPlaying: session.status === "playing",
			currentTime: session.currentTime,
			duration: session.duration,
			volume: session.volume,
			shuffleEnabled: session.shuffleEnabled,
			repeatMode: session.repeatMode,
			playbackError: session.error,
			queueConflict,
			playTrack,
			playRadioStation,
			playRadioCatalogPreview,
			queueTracks,
			playQueueIndex,
			playNext,
			togglePlay: () => engine.togglePlay(),
			toggleShuffle: () => engine.toggleShuffle(),
			cycleRepeatMode: () => engine.cycleRepeatMode(),
			seek: (seconds) => engine.seek(seconds),
			setVolume: (value) => engine.setVolume(value),
			removeFromQueue,
			reorderQueue,
			clearQueue,
			refreshQueue,
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
