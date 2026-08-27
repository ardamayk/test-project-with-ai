import type { Queue, QueueItem } from "@repo/api-client";
import { ApiError } from "@repo/api-client";
import { useCallback, useEffect, useRef, useState } from "react";

export type PlaybackQueueApi = {
	getQueue: () => Promise<Queue>;
	replaceQueue: (trackIds: string[], revision: string) => Promise<Queue>;
	reorderQueue: (itemIds: string[], revision: string) => Promise<Queue>;
	appendQueueItem: (trackId: string, revision: string) => Promise<Queue>;
	removeQueueItem: (itemId: string, revision: string) => Promise<Queue>;
};

export function useSynchronizedQueue(api: PlaybackQueueApi) {
	const apiRef = useRef(api);
	const queueRef = useRef<QueueItem[]>([]);
	const queueRevisionRef = useRef("0");
	const [queue, setQueueState] = useState<QueueItem[]>([]);
	const [queueConflict, setQueueConflict] = useState<string | null>(null);
	apiRef.current = api;

	const applyQueue = useCallback((data: Queue) => {
		queueRef.current = data.items;
		queueRevisionRef.current = data.revision;
		setQueueState(data.items);
	}, []);

	const refreshQueue = useCallback(async () => {
		applyQueue(await apiRef.current.getQueue());
	}, [applyQueue]);

	useEffect(() => {
		void refreshQueue();
	}, [refreshQueue]);

	const retryAppend = useCallback(
		async (trackId: string): Promise<Queue> => {
			const current = await apiRef.current.getQueue();
			applyQueue(current);
			const retried = await apiRef.current.appendQueueItem(
				trackId,
				current.revision,
			);
			applyQueue(retried);
			setQueueConflict(null);
			return retried;
		},
		[applyQueue],
	);

	const appendQueueItem = useCallback(
		async (trackId: string): Promise<Queue> => {
			try {
				const data = await apiRef.current.appendQueueItem(
					trackId,
					queueRevisionRef.current,
				);
				applyQueue(data);
				setQueueConflict(null);
				return data;
			} catch (error) {
				if (!isQueueConflict(error)) throw error;
				return retryAppend(trackId);
			}
		},
		[applyQueue, retryAppend],
	);

	const replaceQueue = useCallback(
		async (trackIds: string[]): Promise<Queue | undefined> => {
			try {
				const data = await apiRef.current.replaceQueue(
					trackIds,
					queueRevisionRef.current,
				);
				applyQueue(data);
				setQueueConflict(null);
				return data;
			} catch (error) {
				if (!isQueueConflict(error)) throw error;
				applyQueue(await apiRef.current.getQueue());
				setQueueConflict(
					"Queue changed in another Playback Client. Review replacement and try again.",
				);
				return undefined;
			}
		},
		[applyQueue],
	);

	const reconcileRemove = useCallback(
		async (itemId: string) => {
			const current = await apiRef.current.getQueue();
			applyQueue(current);
			if (!current.items.some((item) => item.id === itemId)) {
				setQueueConflict(null);
				return;
			}
			const retried = await apiRef.current.removeQueueItem(
				itemId,
				current.revision,
			);
			applyQueue(retried);
			setQueueConflict(null);
		},
		[applyQueue],
	);

	const removeFromQueue = useCallback(
		async (itemId: string) => {
			try {
				const data = await apiRef.current.removeQueueItem(
					itemId,
					queueRevisionRef.current,
				);
				applyQueue(data);
				setQueueConflict(null);
			} catch (error) {
				if (!isQueueConflict(error)) throw error;
				await reconcileRemove(itemId);
			}
		},
		[applyQueue, reconcileRemove],
	);

	const reorderQueue = useCallback(
		async (itemIds: string[]) => {
			try {
				const data = await apiRef.current.reorderQueue(
					itemIds,
					queueRevisionRef.current,
				);
				applyQueue(data);
				setQueueConflict(null);
			} catch (error) {
				if (!isQueueConflict(error)) throw error;
				applyQueue(await apiRef.current.getQueue());
				setQueueConflict(
					"Queue changed in another Playback Client. Review order and try again.",
				);
			}
		},
		[applyQueue],
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
