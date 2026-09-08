import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import type { PlaywrightTestConfig } from "@playwright/test";

export function getTestRunDirectory(): string | undefined {
	if (process.env.EARTHLY_STORAGE_RESOLVED_MODE !== "local") return undefined;
	if (process.env.EARTHLY_RUN_DIR) return process.env.EARTHLY_RUN_DIR;
	const root = process.env.EARTHLY_RUN_ROOT;
	if (!root)
		throw new Error("Missing local test run root; run tests through Mise.");
	const manager = fileURLToPath(
		new URL("../../scripts/storage.mjs", import.meta.url),
	);
	execFileSync(process.execPath, [manager, "prepare"], { stdio: "pipe" });
	const directory = path.join(root, `${Date.now()}-${randomUUID()}`);
	mkdirSync(directory, { recursive: true });
	writeFileSync(
		path.join(directory, "run.json"),
		JSON.stringify({
			version: 1,
			checkout: process.env.EARTHLY_CHECKOUT_ROOT,
			createdAt: Date.now(),
		}),
	);
	process.env.EARTHLY_RUN_DIR = directory;
	return directory;
}

export function getTestArtifacts(
	suite: string,
): Pick<PlaywrightTestConfig, "reporter" | "outputDir"> {
	const directory = getTestRunDirectory();
	if (!directory)
		return {
			reporter: process.env.CI
				? [["list"], ["html", { open: "never" }]]
				: "list",
		};
	return {
		outputDir: path.join(directory, suite, "results"),
		reporter: [
			["list"],
			[
				"html",
				{ open: "never", outputFolder: path.join(directory, suite, "report") },
			],
		],
	};
}
