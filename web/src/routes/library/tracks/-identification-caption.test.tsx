import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { IdentificationCaption } from "./-import-music-dialog";

describe("IdentificationCaption", () => {
	afterEach(() => {
		cleanup();
	});

	it("names MusicBrainz with the AcoustID score and changed fields", () => {
		render(
			<IdentificationCaption
				identification={{
					source: "musicbrainz",
					outcome: "matched",
					acoustIdScore: 0.97,
					recordingId: "rec",
					changedFields: ["title", "artists"],
				}}
			/>,
		);
		expect(screen.getByTestId("identification-caption").textContent).toBe(
			"MusicBrainz · AcoustID 0.97 · changed title, artists",
		);
	});

	it("names file tags with the reason identification did not apply", () => {
		render(
			<IdentificationCaption
				identification={{
					source: "file_tags",
					outcome: "unavailable",
					reason: "AcoustID returned HTTP 503",
				}}
			/>,
		);
		expect(screen.getByTestId("identification-caption").textContent).toBe(
			"File tags · AcoustID returned HTTP 503",
		);
	});

	it("renders nothing for a server that predates identification", () => {
		const { container } = render(
			<IdentificationCaption identification={undefined} />,
		);
		expect(container.textContent).toBe("");
	});
});
