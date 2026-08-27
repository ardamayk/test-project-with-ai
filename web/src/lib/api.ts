import { createApiClient } from "@repo/api-client";
import {
	desktopFetch,
	getCoverBaseUrl,
	getMediaProxyBaseUrl,
	isDesktopClient,
} from "#/desktop/bridge";
import { getApiBaseUrl } from "#/env";

const isDesktop = isDesktopClient();
export const apiClient = createApiClient({
	baseUrl: isDesktop ? "" : getApiBaseUrl(),
	mediaBaseUrl: isDesktop ? getCoverBaseUrl : undefined,
	streamBaseUrl: isDesktop ? getMediaProxyBaseUrl : undefined,
	transport: isDesktop ? desktopFetch : undefined,
});
