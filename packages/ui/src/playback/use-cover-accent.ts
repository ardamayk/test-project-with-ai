import { useEffect, useState } from "react";
import {
	type Accent,
	type AccentThemeMode,
	accentCssVariables,
	extractAccent,
} from "../lib/cover-accent";

const accentCache = new Map<string, Accent | null>();

/** Test hook: forget every cached accent. */
export function clearCoverAccentCache() {
	accentCache.clear();
}

/**
 * Resolves the accent for a cover URL, cached per key (the album id) so a
 * track change within an album never flickers. Returns CSS custom
 * properties for the bar root, or an empty object while unknown/disabled.
 */
export function useCoverAccent({
	cacheKey,
	coverUrl,
	enabled,
	mode,
	loadImage = defaultLoadImage,
}: {
	cacheKey: string | null;
	coverUrl: string | null;
	enabled: boolean;
	mode: AccentThemeMode;
	loadImage?: (url: string) => Promise<HTMLImageElement>;
}): Record<string, string> {
	const [accent, setAccent] = useState<Accent | null>(null);

	useEffect(() => {
		if (!enabled || !cacheKey || !coverUrl) {
			setAccent(null);
			return undefined;
		}
		if (accentCache.has(cacheKey)) {
			setAccent(accentCache.get(cacheKey) ?? null);
			return undefined;
		}
		let cancelled = false;
		loadImage(coverUrl)
			.then((image) => {
				const extracted = extractAccent(image);
				accentCache.set(cacheKey, extracted);
				if (!cancelled) setAccent(extracted);
			})
			.catch(() => {
				accentCache.set(cacheKey, null);
				if (!cancelled) setAccent(null);
			});
		return () => {
			cancelled = true;
		};
	}, [cacheKey, coverUrl, enabled, loadImage]);

	return accentCssVariables(enabled ? accent : null, mode);
}

function defaultLoadImage(url: string): Promise<HTMLImageElement> {
	return new Promise((resolve, reject) => {
		const image = new Image();
		// Needed for an untainted canvas; the server and the desktop cover
		// scheme both answer with CORS headers.
		image.crossOrigin = "anonymous";
		image.onload = () => resolve(image);
		image.onerror = () => reject(new Error("cover could not be loaded"));
		image.src = url;
	});
}
