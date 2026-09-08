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

/** Fill the space after the collection's last column, keeping one shared margin. */
export function useQueueDrawerWidth(
	shellRef: RefObject<HTMLDivElement | null>,
) {
	const [width, setWidth] = useState<number>();
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
			setWidth(calculateQueueDrawerWidth(shellWidth, inset, rootFontSize));
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
	return width;
}
