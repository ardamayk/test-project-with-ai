import type { Queue, QueueEvent, QueueItem } from "@repo/api-client";
import { ApiError } from "@repo/api-client";
import { useCallback, useEffect, useRef, useState } from "react";

export type PlaybackQueueApi = {
	getQueue: () => Promise<Queue>;
	replaceQueue: (trackIds: string[], revision: string) => Promise<Queue>;
	reorderQueue: (itemIds: string[], revision: string) => Promise<Queue>;
	appendQueueItem: (trackId: string, revision: string) => Promise<Queue>;
	removeQueueItem: (itemId: string, revision: string) => Promise<Queue>;
	subscribeQueueEvents?: (
		onEvent: (event: QueueEvent) => void,
		onError?: (error: Error) => void,
	) => () => void;
};

export function useSynchronizedQueue(api: PlaybackQueueApi) {
	const apiRef = useRef(api);
	const queueRef = useRef<QueueItem[]>([]);
	const queueRevisionRef = useRef("0");
	const latestEventSequenceRef = useRef<bigint | null>(null);
	const pendingEventSequencesRef = useRef(new Set<bigint>());
	const nextQueueRequestRef = useRef(0);
	const latestAppliedRequestRef = useRef(0);
	const [queue, setQueueState] = useState<QueueItem[]>([]);
	const [queueConflict, setQueueConflict] = useState<string | null>(null);
	apiRef.current = api;

	const applyQueue = useCallback((data: Queue, requestOrder: number) => {
		if (requestOrder < latestAppliedRequestRef.current) return;
		queueRef.current = data.items;
		queueRevisionRef.current = data.revision;
		latestAppliedRequestRef.current = requestOrder;
		setQueueState(data.items);
	}, []);

	const runQueueRequest = useCallback(
		async (request: () => Promise<Queue>): Promise<Queue> => {
			const requestOrder = ++nextQueueRequestRef.current;
			const data = await request();
			applyQueue(data, requestOrder);
			return data;
		},
		[applyQueue],
	);

	const refreshQueue = useCallback(async () => {
		await runQueueRequest(() => apiRef.current.getQueue());
	}, [runQueueRequest]);

	useEffect(() => {
		void refreshQueue().catch((error) => {
			console.warn("Failed to refresh Queue", { error });
		});
	}, [refreshQueue]);

	useEffect(() => {
		if (!apiRef.current.subscribeQueueEvents) return undefined;
		return apiRef.current.subscribeQueueEvents(
			(event) => {
				const sequence = BigInt(event.sequence);
				if (
					!event.invalidates.includes("queue") ||
					(latestEventSequenceRef.current !== null &&
						sequence <= latestEventSequenceRef.current)
				) {
					return;
				}
				if (pendingEventSequencesRef.current.has(sequence)) return;
				if (event.revision === queueRevisionRef.current) {
					latestEventSequenceRef.current = sequence;
					return;
				}
				pendingEventSequencesRef.current.add(sequence);
				void refreshQueue()
					.then(() => {
						if (
							latestEventSequenceRef.current === null ||
							sequence > latestEventSequenceRef.current
						) {
							latestEventSequenceRef.current = sequence;
						}
					})
					.catch((error) => {
						console.warn("Failed to refresh invalidated Queue", {
							revision: event.revision,
							error,
						});
					})
					.finally(() => {
						pendingEventSequencesRef.current.delete(sequence);
					});
			},
			(error) => {
				console.warn("Queue event stream disconnected", { error });
			},
		);
	}, [refreshQueue]);

	const retryAppend = useCallback(
		async (trackId: string): Promise<Queue> => {
			const current = await runQueueRequest(() => apiRef.current.getQueue());
			const retried = await runQueueRequest(() =>
				apiRef.current.appendQueueItem(trackId, current.revision),
			);
			setQueueConflict(null);
			return retried;
		},
		[runQueueRequest],
	);

	const appendQueueItem = useCallback(
		async (trackId: string): Promise<Queue> => {
			try {
				const data = await runQueueRequest(() =>
					apiRef.current.appendQueueItem(trackId, queueRevisionRef.current),
				);
				setQueueConflict(null);
				return data;
			} catch (error) {
				if (!isQueueConflict(error)) throw error;
				return retryAppend(trackId);
			}
		},
		[retryAppend, runQueueRequest],
	);

	const replaceQueue = useCallback(
		async (trackIds: string[]): Promise<Queue | undefined> => {
			try {
				const data = await runQueueRequest(() =>
					apiRef.current.replaceQueue(trackIds, queueRevisionRef.current),
				);
				setQueueConflict(null);
				return data;
			} catch (error) {
				if (!isQueueConflict(error)) throw error;
				await runQueueRequest(() => apiRef.current.getQueue());
				setQueueConflict(
					"Queue changed in another Playback Client. Review replacement and try again.",
				);
				return undefined;
			}
		},
		[runQueueRequest],
	);

	const reconcileRemove = useCallback(
		async (itemId: string) => {
			const current = await runQueueRequest(() => apiRef.current.getQueue());
			if (!current.items.some((item) => item.id === itemId)) {
				setQueueConflict(null);
				return;
			}
			await runQueueRequest(() =>
				apiRef.current.removeQueueItem(itemId, current.revision),
			);
			setQueueConflict(null);
		},
		[runQueueRequest],
	);

	const removeFromQueue = useCallback(
		async (itemId: string) => {
			try {
				await runQueueRequest(() =>
					apiRef.current.removeQueueItem(itemId, queueRevisionRef.current),
				);
				setQueueConflict(null);
			} catch (error) {
				if (!isQueueConflict(error)) throw error;
				await reconcileRemove(itemId);
			}
		},
		[reconcileRemove, runQueueRequest],
	);

	const reorderQueue = useCallback(
		async (itemIds: string[]) => {
			try {
				await runQueueRequest(() =>
					apiRef.current.reorderQueue(itemIds, queueRevisionRef.current),
				);
				setQueueConflict(null);
			} catch (error) {
				if (!isQueueConflict(error)) throw error;
				await runQueueRequest(() => apiRef.current.getQueue());
				setQueueConflict(
					"Queue changed in another Playback Client. Review order and try again.",
				);
			}
		},
		[runQueueRequest],
	);

	return {
		queue,
		queueRef,
		queueConflict,
		refreshQueue,
		appendQueueItem,
		replaceQueue,
		removeFromQueue,
		reorderQueue,
	};
}

function isQueueConflict(error: unknown): error is ApiError {
	return (
		error instanceof ApiError &&
		error.status === 409 &&
		error.body.code === "queue_revision_conflict"
	);
}
