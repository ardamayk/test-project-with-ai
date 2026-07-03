import type { Track } from "@repo/api-client";
import { useMemo, useState } from "react";
import { filterTracksByText } from "#/lib/filter-tracks";

type TrackCollectionPlayback = {
	playTrack: (trackId: string, queueTrackIds?: string[]) => Promise<void>;
	queueTracks: (trackIds: string[]) => Promise<void>;
};

export function formatTrackCollectionDuration(ms: number): string {
	if (!ms || ms < 0) return "0m";
	const total = Math.floor(ms / 1000);
	const hours = Math.floor(total / 3600);
	const minutes = Math.floor((total % 3600) / 60);
	if (hours > 0) return `${hours}h ${minutes}m`;
	return `${minutes}m`;
}

export function shuffleTrackCollection(tracks: Track[]): Track[] {
	const next = [...tracks];
	for (let i = next.length - 1; i > 0; i -= 1) {
		const j = Math.floor(Math.random() * (i + 1));
		[next[i], next[j]] = [next[j], next[i]];
	}
	return next;
}

export function useTrackCollectionViewState(
	tracks: Track[],
	playback: TrackCollectionPlayback,
) {
	const [search, setSearch] = useState("");
	const visibleTracks = useMemo(
		() => filterTracksByText(tracks, search),
		[tracks, search],
	);
	const visibleTrackIds = useMemo(
		() => visibleTracks.map((track) => track.id),
		[visibleTracks],
	);
	const totalDurationMs = useMemo(
		() => tracks.reduce((sum, track) => sum + (track.durationMs ?? 0), 0),
		[tracks],
	);

	const handlePlay = () => {
		const first = visibleTracks[0];
		if (!first) return;
		void playback.playTrack(first.id, visibleTrackIds);
	};

	const handleShuffle = () => {
		const shuffled = shuffleTrackCollection(visibleTracks);
		const first = shuffled[0];
		if (!first) return;
		void playback.playTrack(
			first.id,
			shuffled.map((track) => track.id),
		);
	};

	const handleQueue = () => {
		void playback.queueTracks(visibleTrackIds);
	};

	return {
		search,
		setSearch,
		visibleTracks,
		visibleTrackIds,
		totalDurationMs,
		handlePlay,
		handleShuffle,
		handleQueue,
	};
}
