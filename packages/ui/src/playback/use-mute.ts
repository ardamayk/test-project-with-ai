import { useCallback, useRef } from "react";

/**
 * Mute that remembers the last audible level so unmuting restores it. The
 * engines only know a single volume number, so this lives above them and is
 * shared by the speaker button and the M shortcut.
 */
export function useMute(volume: number, setVolume: (value: number) => void) {
	const lastAudibleVolumeRef = useRef(volume > 0 ? volume : 1);
	if (volume > 0) lastAudibleVolumeRef.current = volume;

	const toggleMute = useCallback(() => {
		setVolume(volume <= 0 ? lastAudibleVolumeRef.current : 0);
	}, [setVolume, volume]);

	return { isMuted: volume <= 0, toggleMute };
}
