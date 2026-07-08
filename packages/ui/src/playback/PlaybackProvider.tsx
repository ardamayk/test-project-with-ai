import type {
	Playlist,
	PlaylistDetail,
	PlaylistList,
	Queue,
	QueueItem,
	RadioNowPlaying,
	RadioSearchResult,
	RadioStation,
	Track,
} from "@repo/api-client";
import Hls from "hls.js";
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

export type RepeatMode = "off" | "once" | "loop";

export type PlaybackQueueApi = {
	getQueue: () => Promise<Queue>;
	replaceQueue: (trackIds: string[]) => Promise<Queue>;
	appendQueueItem: (trackId: string) => Promise<Queue>;
	removeQueueItem: (itemId: string) => Promise<Queue>;
};

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
	clearQueue: () => Promise<void>;
	refreshQueue: () => Promise<void>;
	getAlbumCoverUrl: (albumId: string) => string;
};

const PlaybackContext = createContext<PlaybackContextValue | null>(null);
const PlaylistLibraryContext = createContext<PlaylistLibraryApi | null>(null);

export function PlaybackProvider({
	children,
	api,
}: {
	children: ReactNode;
	api: PlaybackApi;
}) {
	const audioRef = useRef<HTMLAudioElement | null>(null);
	const hlsRef = useRef<Hls | null>(null);
	const queueRef = useRef<QueueItem[]>([]);
	const currentTrackRef = useRef<Track | null>(null);
	const currentRadioStationRef = useRef<RadioStation | null>(null);
	const repeatModeRef = useRef<RepeatMode>("off");
	const apiRef = useRef(api);
	apiRef.current = api;

	const [queue, setQueue] = useState<QueueItem[]>([]);
	const [currentTrack, setCurrentTrack] = useState<Track | null>(null);
	const [currentRadioStation, setCurrentRadioStation] =
		useState<RadioStation | null>(null);
	const [radioNowPlaying, setRadioNowPlaying] =
		useState<RadioNowPlaying | null>(null);
	const [isPlaying, setIsPlaying] = useState(false);
	const [currentTime, setCurrentTime] = useState(0);
	const [duration, setDuration] = useState(0);
	const [volume, setVolumeState] = useState(0.8);
	const [shuffleEnabled, setShuffleEnabled] = useState(false);
	const [repeatMode, setRepeatMode] = useState<RepeatMode>("off");

	queueRef.current = queue;
	currentTrackRef.current = currentTrack;
	currentRadioStationRef.current = currentRadioStation;
	repeatModeRef.current = repeatMode;

	const refreshQueue = useCallback(async () => {
		const data = await apiRef.current.getQueue();
		setQueue(data.items);
	}, []);

	const destroyHls = useCallback(() => {
		hlsRef.current?.destroy();
		hlsRef.current = null;
	}, []);

	const setAudioSource = useCallback(
		(audio: HTMLAudioElement, playbackUrl: string, sourceUrl = playbackUrl) => {
			destroyHls();
			if (isHlsStream(sourceUrl) && Hls.isSupported()) {
				const hls = new Hls();
				hlsRef.current = hls;
				hls.loadSource(sourceUrl);
				hls.attachMedia(audio);
				return;
			}
			if (isHlsStream(sourceUrl) && canPlayNativeHls(audio)) {
				audio.src = sourceUrl;
				return;
			}
			audio.src = playbackUrl;
		},
		[destroyHls],
	);

	const playTrackInternal = useCallback(
		async (trackId: string, queueOverride?: QueueItem[]) => {
			const items = queueOverride ?? queueRef.current;
			const item = items.find(
				(entry) => entry.track.id === trackId || entry.trackId === trackId,
			);
			if (item) {
				setCurrentTrack(item.track);
				currentRadioStationRef.current = null;
				setCurrentRadioStation(null);
				setRadioNowPlaying(null);
				setDuration(
					item.track.durationMs > 0 ? item.track.durationMs / 1000 : 0,
				);
			}

			const audio = audioRef.current;
			if (!audio) return;

			setAudioSource(audio, apiRef.current.getStreamUrl(trackId));
			setCurrentTime(0);
			try {
				await audio.play();
				setIsPlaying(true);
			} catch {
				setIsPlaying(false);
			}
		},
		[setAudioSource],
	);

	const refreshRadioNowPlaying = useCallback(async () => {
		const station = currentRadioStationRef.current;
		if (!station) return;
		try {
			const data = await apiRef.current.getRadioNowPlaying(station.id);
			setRadioNowPlaying(data);
		} catch (error) {
			console.warn("Failed to refresh radio now playing", {
				stationId: station.id,
				error,
			});
		}
	}, []);

	useEffect(() => {
		const audio = new Audio();
		audio.volume = volume;
		audioRef.current = audio;

		const onTimeUpdate = () => setCurrentTime(audio.currentTime);
		const onDurationChange = () => {
			const next = audio.duration;
			if (Number.isFinite(next) && next > 0) {
				setDuration(next);
			}
		};
		const onLoadedMetadata = () => onDurationChange();
		const onPlay = () => setIsPlaying(true);
		const onPause = () => setIsPlaying(false);
		const onEnded = () => {
			setIsPlaying(false);
			if (currentRadioStationRef.current) {
				return;
			}
			const mode = repeatModeRef.current;
			if (mode === "once" || mode === "loop") {
				audio.currentTime = 0;
				setCurrentTime(0);
				if (mode === "once") {
					repeatModeRef.current = "off";
					setRepeatMode("off");
				}
				void audio.play();
				return;
			}

			const items = queueRef.current;
			const playing = currentTrackRef.current;
			const idx = items.findIndex((item) => item.track.id === playing?.id);
			if (idx >= 0 && idx < items.length - 1) {
				void playTrackInternal(items[idx + 1].track.id);
			}
		};

		audio.addEventListener("timeupdate", onTimeUpdate);
		audio.addEventListener("durationchange", onDurationChange);
		audio.addEventListener("loadedmetadata", onLoadedMetadata);
		audio.addEventListener("ended", onEnded);
		audio.addEventListener("play", onPlay);
		audio.addEventListener("pause", onPause);

		void refreshQueue();

		return () => {
			audio.pause();
			destroyHls();
			audio.removeEventListener("timeupdate", onTimeUpdate);
			audio.removeEventListener("durationchange", onDurationChange);
			audio.removeEventListener("loadedmetadata", onLoadedMetadata);
			audio.removeEventListener("ended", onEnded);
			audio.removeEventListener("play", onPlay);
			audio.removeEventListener("pause", onPause);
			audioRef.current = null;
		};
	}, [destroyHls, playTrackInternal, refreshQueue]);

	useEffect(() => {
		if (!currentRadioStation) return undefined;
		void refreshRadioNowPlaying();
		const intervalId = window.setInterval(() => {
			void refreshRadioNowPlaying();
		}, 30000);
		return () => window.clearInterval(intervalId);
	}, [currentRadioStation, refreshRadioNowPlaying]);

	const playTrack = useCallback(
		async (trackId: string, queueTrackIds?: string[]) => {
			let nextQueue = queueRef.current;
			if (queueTrackIds) {
				const data = await apiRef.current.replaceQueue(queueTrackIds);
				nextQueue = data.items;
				setQueue(data.items);
			}
			const trackInQueue = nextQueue.some((item) => item.track.id === trackId);
			if (!trackInQueue) {
				const data = await apiRef.current.appendQueueItem(trackId);
				nextQueue = data.items;
				setQueue(data.items);
			}
			await playTrackInternal(trackId, nextQueue);
		},
		[playTrackInternal],
	);

	const playRadioStation = useCallback(
		async (station: RadioStation) => {
			const audio = audioRef.current;
			if (!audio) return;

			setCurrentTrack(null);
			currentRadioStationRef.current = station;
			setCurrentRadioStation(station);
			setRadioNowPlaying(station.lastNowPlaying ?? null);
			setCurrentTime(0);
			setDuration(0);

			setAudioSource(
				audio,
				apiRef.current.getRadioStationStreamUrl(station.id),
				station.streamUrl,
			);
			try {
				await audio.play();
				setIsPlaying(true);
			} catch (error) {
				console.warn("Failed to start radio station playback", {
					stationId: station.id,
					error,
				});
				setIsPlaying(false);
			}

			await refreshRadioNowPlaying();
		},
		[refreshRadioNowPlaying, setAudioSource],
	);

	const playRadioCatalogPreview = useCallback(
		async (result: RadioSearchResult) => {
			const audio = audioRef.current;
			if (!audio) return;

			const station: RadioStation = {
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

			setCurrentTrack(null);
			currentRadioStationRef.current = station;
			setCurrentRadioStation(station);
			setRadioNowPlaying(null);
			setCurrentTime(0);
			setDuration(0);

			setAudioSource(
				audio,
				apiRef.current.getRadioCatalogPreviewStreamUrl(result.stationUuid),
				result.streamUrl,
			);
			try {
				await audio.play();
				setIsPlaying(true);
			} catch (error) {
				console.warn("Failed to start radio catalog preview", {
					stationUuid: result.stationUuid,
					error,
				});
				setIsPlaying(false);
				throw error;
			}

			await refreshRadioNowPlaying();
		},
		[refreshRadioNowPlaying, setAudioSource],
	);

	const queueTracks = useCallback(async (trackIds: string[]) => {
		let nextQueue = queueRef.current;
		for (const trackId of trackIds) {
			const data = await apiRef.current.appendQueueItem(trackId);
			nextQueue = data.items;
		}
		setQueue(nextQueue);
	}, []);

	const playQueueIndex = useCallback(
		async (index: number) => {
			const item = queueRef.current[index];
			if (!item) return;
			await playTrackInternal(item.track.id);
		},
		[playTrackInternal],
	);

	const playNext = useCallback(async (trackId: string) => {
		const items = queueRef.current;
		const playing = currentTrackRef.current;
		const trackIds = items
			.map((item) => item.track.id)
			.filter((id) => id !== trackId);
		const currentIndex = playing
			? items.findIndex((item) => item.track.id === playing.id)
			: -1;
		const insertAt = currentIndex >= 0 ? currentIndex + 1 : trackIds.length;
		trackIds.splice(insertAt, 0, trackId);
		const data = await apiRef.current.replaceQueue(trackIds);
		setQueue(data.items);
	}, []);

	const togglePlay = useCallback(() => {
		const audio = audioRef.current;
		if (!audio) return;
		if (audio.paused) {
			void audio.play();
		} else {
			audio.pause();
		}
	}, []);

	const toggleShuffle = useCallback(() => {
		setShuffleEnabled((enabled) => !enabled);
	}, []);

	const cycleRepeatMode = useCallback(() => {
		setRepeatMode((mode) => {
			if (mode === "off") return "once";
			if (mode === "once") return "loop";
			return "off";
		});
	}, []);

	const seek = useCallback((seconds: number) => {
		const audio = audioRef.current;
		if (!audio) return;
		audio.currentTime = seconds;
		setCurrentTime(seconds);
	}, []);

	const setVolume = useCallback((value: number) => {
		const clamped = Math.min(1, Math.max(0, value));
		setVolumeState(clamped);
		if (audioRef.current) {
			audioRef.current.volume = clamped;
		}
	}, []);

	const removeFromQueue = useCallback(async (itemId: string) => {
		const data = await apiRef.current.removeQueueItem(itemId);
		setQueue(data.items);
	}, []);

	const clearQueue = useCallback(async () => {
		const data = await apiRef.current.replaceQueue([]);
		setQueue(data.items);
		setCurrentTrack(null);
		currentRadioStationRef.current = null;
		setCurrentRadioStation(null);
		setRadioNowPlaying(null);
		setIsPlaying(false);
		if (audioRef.current) {
			audioRef.current.pause();
			destroyHls();
			audioRef.current.removeAttribute("src");
		}
	}, [destroyHls]);

	const value = useMemo(
		() => ({
			queue,
			currentTrack,
			currentRadioStation,
			radioNowPlaying,
			isPlaying,
			currentTime,
			duration,
			volume,
			shuffleEnabled,
			repeatMode,
			playTrack,
			playRadioStation,
			playRadioCatalogPreview,
			queueTracks,
			playQueueIndex,
			playNext,
			togglePlay,
			toggleShuffle,
			cycleRepeatMode,
			seek,
			setVolume,
			removeFromQueue,
			clearQueue,
			refreshQueue,
			getAlbumCoverUrl: (albumId: string) =>
				apiRef.current.getAlbumCoverUrl(albumId),
		}),
		[
			queue,
			currentTrack,
			currentRadioStation,
			radioNowPlaying,
			isPlaying,
			currentTime,
			duration,
			volume,
			shuffleEnabled,
			repeatMode,
			playTrack,
			playRadioStation,
			playRadioCatalogPreview,
			queueTracks,
			playQueueIndex,
			playNext,
			togglePlay,
			toggleShuffle,
			cycleRepeatMode,
			seek,
			setVolume,
			removeFromQueue,
			clearQueue,
			refreshQueue,
		],
	);

	const playlistLibrary = useMemo<PlaylistLibraryApi>(
		() => ({
			listPlaylists: () => apiRef.current.listPlaylists(),
			getPlaylist: (playlistId: string) =>
				apiRef.current.getPlaylist(playlistId),
			createPlaylist: (name: string) => apiRef.current.createPlaylist(name),
			addPlaylistTrack: (playlistId: string, trackId: string) =>
				apiRef.current.addPlaylistTrack(playlistId, trackId),
			removePlaylistTrack: (playlistId: string, trackId: string) =>
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
	const ctx = useContext(PlaybackContext);
	if (!ctx) {
		throw new Error("usePlayback must be used within PlaybackProvider");
	}
	return ctx;
}

export function usePlaylistLibrary() {
	const ctx = useContext(PlaylistLibraryContext);
	if (!ctx) {
		throw new Error("usePlaylistLibrary must be used within PlaybackProvider");
	}
	return ctx;
}

function isHlsStream(url: string) {
	try {
		return new URL(url, window.location.href).pathname
			.toLowerCase()
			.endsWith(".m3u8");
	} catch {
		return url.toLowerCase().includes(".m3u8");
	}
}

function canPlayNativeHls(audio: HTMLAudioElement) {
	return Boolean(
		audio.canPlayType?.("application/vnd.apple.mpegurl") ||
			audio.canPlayType?.("application/x-mpegurl"),
	);
}
