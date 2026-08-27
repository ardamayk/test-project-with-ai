import type { PlaybackEngine } from "@repo/ui";
import { isDesktopClient } from "#/desktop/bridge";
import { BrowserPlaybackEngine } from "./BrowserPlaybackEngine";
import { DesktopPlaybackEngine } from "./DesktopPlaybackEngine";
import { DesktopSourceRoutingPlaybackEngine } from "./DesktopSourceRoutingPlaybackEngine";

export function createPlaybackEngine(): PlaybackEngine {
	return isDesktopClient()
		? new DesktopSourceRoutingPlaybackEngine(
				new DesktopPlaybackEngine(),
				() => new BrowserPlaybackEngine(),
			)
		: new BrowserPlaybackEngine();
}
