import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { ArtistsPage } from "./-artists-page";

const artistsSearchSchema = z.object({
	q: z.string().optional(),
});

export const Route = createFileRoute("/library/artists/")({
	validateSearch: artistsSearchSchema,
	component: ArtistsRoute,
});

function ArtistsRoute() {
	const { q } = Route.useSearch();
	return <ArtistsPage initialSearch={q} />;
}
