import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { apiClient } from "#/lib/api";
import { invalidateLibraryCache } from "#/lib/invalidate-library-cache";
import { libraryQueryKeys } from "#/lib/library-query-keys";

export const libraryScanStatusQueryKey = libraryQueryKeys.scanStatus;

export function useLibraryScanStatus() {
	return useQuery({
		queryKey: libraryScanStatusQueryKey,
		queryFn: () => apiClient.getLibraryScanStatus(),
		refetchInterval: (query) =>
			query.state.data?.status === "running" ? 2000 : false,
	});
}

/** Refetch library data after a scan finishes (app-wide). Mount once in root layout. */
export function useLibraryScanSync() {
	const queryClient = useQueryClient();
	const previousStatus = useRef<string | undefined>(undefined);
	const scanStatus = useLibraryScanStatus();
	const status = scanStatus.data?.status;

	useEffect(() => {
		const prev = previousStatus.current;
		if (prev === "running" && (status === "completed" || status === "failed")) {
			void invalidateLibraryCache(queryClient);
		}
		previousStatus.current = status;
	}, [queryClient, status]);

	return scanStatus;
}
