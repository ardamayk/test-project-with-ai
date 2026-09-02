import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const fastWorkflow = readFileSync(
	new URL("../.github/workflows/pr-fast-gate.yml", import.meta.url),
	"utf8",
);
const integrationWorkflow = readFileSync(
	new URL("../.github/workflows/pr-integration-gate.yml", import.meta.url),
	"utf8",
);

function getJob(workflow, jobName) {
	const jobStart = workflow.indexOf(`  ${jobName}:\n`);
	assert.notEqual(jobStart, -1, `missing ${jobName} job`);
	const nextJobMatch = /^ {2}[\w-]+:\n/gm.exec(
		workflow.slice(jobStart + jobName.length + 4),
	);
	const nextJob = nextJobMatch
		? jobStart + jobName.length + 4 + nextJobMatch.index
		: -1;
	return workflow.slice(jobStart, nextJob === -1 ? undefined : nextJob);
}

function assertMainTrigger(workflow) {
	assert.match(workflow, /push:\n\s+branches: \[main\]/);
	assert.match(
		workflow,
		/cancel-in-progress: \$\{\{ github\.event_name == 'pull_request' \}\}/,
	);
}

test("main updates run uncancelled full fast and integration verification", () => {
	assertMainTrigger(fastWorkflow);
	assertMainTrigger(integrationWorkflow);

	for (const workflow of [fastWorkflow, integrationWorkflow]) {
		assert.doesNotMatch(workflow, /runs-on: (?!ubuntu-24\.04)/);
	}

	for (const jobName of [
		"workspace",
		"music-server",
		"desktop",
		"generated-drift",
	]) {
		assert.match(getJob(fastWorkflow, jobName), /github\.event_name == 'push'/);
	}

	for (const jobName of ["web-e2e", "hls", "desktop-unit", "real-mpv"]) {
		assert.match(
			getJob(integrationWorkflow, jobName),
			/github\.event_name == 'push'/,
		);
	}
});

test("trusted main verification restores and publishes every agreed cache", () => {
	for (const cacheName of ["pnpm", "Turbo", "Go", "golangci-lint", "Cargo"]) {
		assert.match(
			fastWorkflow,
			new RegExp(`Restore ${cacheName}[^\\n]* cache`, "i"),
		);
		assert.match(
			fastWorkflow,
			new RegExp(`Publish ${cacheName}[^\\n]* cache`, "i"),
		);
	}

	for (const cacheName of ["Playwright browser", "verified pinned mpv"]) {
		assert.match(
			integrationWorkflow,
			new RegExp(`Restore ${cacheName}[^\\n]* cache`, "i"),
		);
		assert.match(
			integrationWorkflow,
			new RegExp(`Publish ${cacheName}[^\\n]* cache`, "i"),
		);
	}
});

test("main verification builds and retains separate production artifacts", () => {
	const webJob = getJob(integrationWorkflow, "web-e2e");
	const serverJob = getJob(integrationWorkflow, "hls");
	const desktopJob = getJob(integrationWorkflow, "real-mpv");

	assert.match(webJob, /mise run web:build/);
	assert.match(webJob, /name: main-web-client/);
	assert.match(serverJob, /mise run server:build/);
	assert.match(serverJob, /name: main-music-server/);
	assert.match(serverJob, /name: main-product-docs/);
	assert.match(desktopJob, /mise run desktop:build/);
	assert.match(desktopJob, /name: main-desktop-client/);

	for (const job of [webJob, serverJob, desktopJob]) {
		assert.match(job, /retention-days: 14/);
		assert.match(job, /GITHUB_STEP_SUMMARY/);
		assert.match(job, /if: failure\(\)/);
	}
});

test("main verification consumes committed generated clients", () => {
	assert.match(
		getJob(fastWorkflow, "generated-drift"),
		/mise run generate:check/,
	);
	for (const workflow of [fastWorkflow, integrationWorkflow]) {
		assert.doesNotMatch(workflow, /(?:mise run|pnpm) generate(?:\s|$)/);
	}
});
