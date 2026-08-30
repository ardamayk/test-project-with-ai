import { createFileRoute } from "@tanstack/react-router";
import { PlaylistsPage } from "./-playlists-page";

export const Route = createFileRoute("/playlists/")({
	component: PlaylistsPage,
});
