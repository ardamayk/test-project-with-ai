import { createFileRoute } from "@tanstack/react-router";
import { RadioDiscoverPage } from "./-radio-discover-page";

export const Route = createFileRoute("/radio/discover")({
	component: RadioDiscoverPage,
});
