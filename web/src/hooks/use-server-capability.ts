import { useQuery } from "@tanstack/react-query";
import { apiClient } from "#/lib/api";

export function useServerCapability(capability: string): boolean {
	const health = useQuery({
		queryKey: ["health"],
		queryFn: () => apiClient.getHealth(),
		staleTime: Number.POSITIVE_INFINITY,
	});
	return health.data?.capabilities.includes(capability) ?? false;
}
