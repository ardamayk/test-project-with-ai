import { createFileRoute } from "@tanstack/react-router";
import { RadioPage } from "./-radio-page";

export const Route = createFileRoute("/radio/")({
	component: RadioPage,
});
