import { createFileRoute } from "@tanstack/react-router";
import { TracksPage } from "./-tracks-page";

export const Route = createFileRoute("/library/tracks/")({
	component: TracksPage,
});
