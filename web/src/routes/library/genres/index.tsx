import { createFileRoute } from "@tanstack/react-router";
import { GenresPage } from "./-genres-page";

export const Route = createFileRoute("/library/genres/")({
	component: GenresPage,
});
