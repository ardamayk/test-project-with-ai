import { createFileRoute } from "@tanstack/react-router";
import { AlbumsPage } from "./-albums-page";

export const Route = createFileRoute("/library/albums/")({
	component: AlbumsPage,
});
