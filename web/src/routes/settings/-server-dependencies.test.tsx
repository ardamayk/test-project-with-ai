import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { ServerDependenciesSection } from "./-server-dependencies";

const health = {
	status: "ok" as const,
	version: "0.1.0",
	capabilities: ["api.v1", "recording-identification.v1"],
	dependencies: [
		{ name: "ffmpeg", required: true, available: true, version: "7.1.1" },
		{ name: "ffprobe", required: true, available: true, version: "7.1.1" },
		{ name: "fpcalc", required: false, available: false },
	],
	recordingIdentification: {
		status: "missing_fpcalc" as const,
		acoustIdKeySource: "missing" as const,
	},
};

describe("ServerDependenciesSection", () => {
	afterEach(() => {
		cleanup();
	});

	it("lists every Server Dependency with its version and requirement", () => {
		render(<ServerDependenciesSection health={health} desktopMpv={null} />);

		const ffmpeg = screen.getByRole("row", { name: /ffmpeg/ });
		expect(ffmpeg.textContent).toContain("7.1.1");
		expect(ffmpeg.textContent).toContain("Required");
		expect(ffmpeg.textContent).toContain("Installed");

		const fpcalc = screen.getByRole("row", { name: /fpcalc/ });
		expect(fpcalc.textContent).toContain("Optional");
		expect(fpcalc.textContent).toContain("Missing");
		expect(fpcalc.textContent).not.toContain("Required");
	});

	it("explains the Recording Identification status and key source", () => {
		render(<ServerDependenciesSection health={health} desktopMpv={null} />);

		expect(
			screen.getByText(/Recording Identification/).parentElement?.textContent,
		).toContain("Unavailable: fpcalc is not installed");
		expect(
			screen.getByText(/AcoustID key/).parentElement?.textContent,
		).toContain("Not configured");
	});

	it("shows Recording Identification as active when enabled with an embedded key", () => {
		render(
			<ServerDependenciesSection
				health={{
					...health,
					recordingIdentification: {
						status: "enabled",
						acoustIdKeySource: "embedded",
					},
				}}
				desktopMpv={null}
			/>,
		);

		expect(
			screen.getByText(/Recording Identification/).parentElement?.textContent,
		).toContain("Active");
		expect(
			screen.getByText(/AcoustID key/).parentElement?.textContent,
		).toContain("Embedded in this release");
	});

	it("omits the mpv row on the Web Client and shows it on the Desktop Client", () => {
		const { unmount } = render(
			<ServerDependenciesSection health={health} desktopMpv={null} />,
		);
		expect(screen.queryByRole("row", { name: /mpv/ })).toBeNull();
		unmount();

		render(
			<ServerDependenciesSection
				health={health}
				desktopMpv={{ available: true, version: "0.40.0" }}
			/>,
		);
		const mpv = screen.getByRole("row", { name: /mpv/ });
		expect(mpv.textContent).toContain("0.40.0");
		expect(mpv.textContent).toContain("Desktop Client");
	});

	it("renders a loading state without health data", () => {
		render(<ServerDependenciesSection health={undefined} desktopMpv={null} />);

		expect(screen.getByText(/Loading server dependencies/)).toBeTruthy();
	});
});
