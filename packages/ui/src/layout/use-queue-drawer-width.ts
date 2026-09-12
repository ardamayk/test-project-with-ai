import { type RefObject, useLayoutEffect, useState } from "react";

// Match the fixed collection cards and gap used by the host's collection grid.
const COLLECTION_CARD_WIDTH_PX = 250;
const COLLECTION_GRID_GAP_REM = 0.75;
const MAX_COLLECTION_COLUMNS = 6;
const QUEUE_MIN_WIDTH_REM = 18;
const QUEUE_GAP_REM = 1.5;
const DESKTOP_BREAKPOINT_PX = 1024;

export function calculateQueueDrawerWidth(
	shellWidth: number,
	inset: number,
	rootFontSize: number,
): number {
	const minimumWidth = QUEUE_MIN_WIDTH_REM * rootFontSize;
	const availableWidth = Math.max(0, shellWidth - 2 * inset);
	if (shellWidth < DESKTOP_BREAKPOINT_PX)
		return Math.min(minimumWidth, availableWidth);
	const margin = QUEUE_GAP_REM * rootFontSize;
	const collectionGap = COLLECTION_GRID_GAP_REM * rootFontSize;
	const columns = Math.min(
		MAX_COLLECTION_COLUMNS,
		Math.floor(
			(availableWidth - minimumWidth - margin + collectionGap) /
				(COLLECTION_CARD_WIDTH_PX + collectionGap),
		),
	);
	if (columns < 1) return Math.min(minimumWidth, availableWidth);
	const collectionWidth =
		columns * COLLECTION_CARD_WIDTH_PX + (columns - 1) * collectionGap;
	return availableWidth - collectionWidth - margin;
}

export function calculateLibrarySearchBounds(
	shellWidth: number,
	inset: number,
	rootFontSize: number,
) {
	const gap = COLLECTION_GRID_GAP_REM * rootFontSize;
	const pitch = COLLECTION_CARD_WIDTH_PX + gap;
	const columns = Math.floor(
		(Math.max(0, shellWidth - 2 * inset) + gap) / pitch,
	);
	if (columns < 6) {
		// ponytail: fewer than six columns cannot span both third cards; use viewport gutters.
		return {
			width: Math.max(0, Math.min(52 * rootFontSize, shellWidth - 32)),
			center: shellWidth / 2,
		};
	}
	return {
		width: (columns - 5) * pitch,
		center: inset + (columns * pitch - gap) / 2,
	};
}

/** Measure Queue and Search from the full shell, independently of Queue visibility. */
export function useQueueDrawerWidth(
	shellRef: RefObject<HTMLDivElement | null>,
) {
	const [metrics, setMetrics] = useState<{
		queueWidth: number;
		searchWidth: number;
		searchCenter: number;
	}>();
	useLayoutEffect(() => {
		const shell = shellRef.current;
		const navigation = shell?.querySelector("header");
		if (!shell || !navigation) return;
		const measure = () => {
			const shellWidth = shell.getBoundingClientRect().width;
			if (shellWidth === 0) return;
			const inset =
				Number.parseFloat(getComputedStyle(navigation).paddingRight) || 0;
			const rootFontSize =
				Number.parseFloat(
					getComputedStyle(document.documentElement).fontSize,
				) || 16;
			const search = calculateLibrarySearchBounds(
				shellWidth,
				inset,
				rootFontSize,
			);
			setMetrics({
				queueWidth: calculateQueueDrawerWidth(shellWidth, inset, rootFontSize),
				searchWidth: search.width,
				searchCenter: shell.getBoundingClientRect().left + search.center,
			});
		};
		measure();
		const observer = new ResizeObserver(measure);
		observer.observe(shell);
		window.addEventListener("resize", measure);
		return () => {
			observer.disconnect();
			window.removeEventListener("resize", measure);
		};
	}, [shellRef]);
	return metrics;
}
