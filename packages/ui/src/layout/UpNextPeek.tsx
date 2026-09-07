import { SkipForward } from "lucide-react";
import { usePlayback } from "../playback/PlaybackProvider";
import { AlbumArt } from "./AlbumArt";

/**
 * The Track that follows the current one, shown beside the now-playing
 * info. Clicking it jumps ahead; hidden on narrow bars and while radio
 * plays, where there is no next Track.
 */
export function UpNextPeek() {
	const { queue, currentTrack, playQueueIndex, getAlbumCoverUrl } =
		usePlayback();
	if (!currentTrack) return null;
	const currentIndex = queue.findIndex(
		(item) => item.track.id === currentTrack.id,
	);
	const next = currentIndex >= 0 ? queue[currentIndex + 1] : undefined;
	if (!next) return null;
	return (
		<button
			type="button"
			data-player-control
			data-testid="up-next"
			className="@[26rem]/now-playing:flex ml-auto hidden w-[9rem] shrink-0 items-center gap-2 rounded-md px-2 py-1 text-left text-player-foreground hover:bg-[var(--player-pill)]"
			title={`Up next: ${next.track.title}`}
			aria-label={`Up next: ${next.track.title} by ${next.track.artistName}. Play now`}
			onClick={() => void playQueueIndex(currentIndex + 1)}
		>
			<AlbumArt
				coverUrl={getAlbumCoverUrl(next.track.albumId)}
				title={next.track.title}
				className="size-8 shrink-0 rounded text-[10px]"
			/>
			<span className="min-w-0">
				<span className="flex items-center gap-1 text-[10px] text-caption uppercase tracking-wide">
					<SkipForward className="size-2.5" aria-hidden />
					Up next
				</span>
				<span className="block truncate text-xs">{next.track.title}</span>
			</span>
		</button>
	);
}
