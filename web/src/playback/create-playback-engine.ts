import type { PlaybackEngine } from "@repo/ui";
import { isDesktopClient } from "#/desktop/bridge";
import { BrowserPlaybackEngine } from "./BrowserPlaybackEngine";
import { DesktopPlaybackEngine } from "./DesktopPlaybackEngine";

export function createPlaybackEngine(): PlaybackEngine {
	return isDesktopClient()
		? new DesktopPlaybackEngine()
		: new BrowserPlaybackEngine();
}
