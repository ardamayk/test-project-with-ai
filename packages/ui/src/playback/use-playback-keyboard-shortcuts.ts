import { useEffect } from "react";

export interface PlaybackKeyboardActions {
	togglePlay: () => void;
	navigatePrevious: () => void;
	navigateNext: () => void;
}

const EDITABLE_INPUT_TYPES = new Set([
	"text",
	"search",
	"email",
	"url",
	"password",
	"number",
	"tel",
]);

/**
 * Space must not steal keystrokes from places where the user is typing or
 * where the browser already gives Space a meaning (buttons, links, sliders,
 * checkboxes). Those controls keep their native behaviour.
 */
export function shouldIgnorePlaybackShortcut(target: EventTarget | null) {
	if (!(target instanceof HTMLElement)) return false;
	if (target.isContentEditable) return true;
	if (target instanceof HTMLTextAreaElement) return true;
	if (target instanceof HTMLSelectElement) return true;
	if (target instanceof HTMLInputElement) {
		return EDITABLE_INPUT_TYPES.has(target.type) || target.type === "range";
	}
	if (
		target instanceof HTMLButtonElement ||
		target instanceof HTMLAnchorElement
	) {
		return true;
	}
	const role = target.getAttribute("role");
	return (
		role === "button" ||
		role === "slider" ||
		role === "checkbox" ||
		role === "switch" ||
		role === "menuitem" ||
		role === "option" ||
		role === "textbox"
	);
}

/**
 * Global playback shortcuts: Space toggles play/pause, and the keyboard media
 * keys map to their obvious actions. Mount once, next to the player bar.
 */
export function usePlaybackKeyboardShortcuts({
	togglePlay,
	navigatePrevious,
	navigateNext,
}: PlaybackKeyboardActions) {
	useEffect(() => {
		const handleKeyDown = (event: KeyboardEvent) => {
			if (event.defaultPrevented || event.repeat) return;
			switch (event.key) {
				case " ":
					if (event.ctrlKey || event.metaKey || event.altKey) return;
					if (shouldIgnorePlaybackShortcut(event.target)) return;
					event.preventDefault();
					togglePlay();
					return;
				case "MediaPlayPause":
					event.preventDefault();
					togglePlay();
					return;
				case "MediaTrackNext":
					event.preventDefault();
					navigateNext();
					return;
				case "MediaTrackPrevious":
					event.preventDefault();
					navigatePrevious();
					return;
				default:
					return;
			}
		};
		document.addEventListener("keydown", handleKeyDown);
		return () => document.removeEventListener("keydown", handleKeyDown);
	}, [togglePlay, navigatePrevious, navigateNext]);
}
