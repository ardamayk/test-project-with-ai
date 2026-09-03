import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const mainWorkflow = readFileSync(
	new URL("../.github/workflows/main.yml", import.meta.url),
	"utf8",
);
const fastWorkflow = readFileSync(
	new URL("../.github/workflows/pr-fast-gate.yml", import.meta.url),
	"utf8",
);
const integrationWorkflow = readFileSync(
	new URL("../.github/workflows/pr-integration-gate.yml", import.meta.url),
	"utf8",
);
const nightlyWorkflow = readFileSync(
	new URL("../.github/workflows/nightly.yml", import.meta.url),
	"utf8",
);
const pinnedMpvBuilder = readFileSync(
	new URL("build-pinned-mpv.sh", import.meta.url),
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

test("main verification is isolated from pull-request workflows and never cancelled", () => {
	assert.match(mainWorkflow, /name: Main/);
	assert.match(mainWorkflow, /push:\n\s+branches: \[main\]/);
	assert.match(mainWorkflow, /cancel-in-progress: false/);
	assert.doesNotMatch(mainWorkflow, /pull_request:/);
	assert.doesNotMatch(fastWorkflow, /push:/);
	assert.doesNotMatch(integrationWorkflow, /push:/);
	assert.doesNotMatch(mainWorkflow, /runs-on: (?!ubuntu-24\.04)/);
});

test("main verification runs every public static, unit, and integration task", () => {
	for (const task of [
		"workspace:check",
		"workspace:typecheck",
		"workspace:test",
		"git-hooks:test",
		"classifier:test",
		"main-workflows:test",
		"web:test:e2e",
		"server:format:check",
		"server:lint",
		"server:test",
		"server:test:hls",
		"desktop:format:check",
		"desktop:lint",
		"desktop:test",
		"desktop:test:mpv",
		"desktop:test:import-parity",
		"generate:check",
	]) {
		assert.match(mainWorkflow, new RegExp(`mise run ${task}`));
	}

	for (const jobName of [
		"workspace",
		"music-server",
		"generated-drift",
		"desktop",
	]) {
		assert.match(getJob(mainWorkflow, jobName), /GITHUB_STEP_SUMMARY/);
	}
	assert.match(mainWorkflow, /mise run clean-room:test/);
	assert.match(fastWorkflow, /mise run clean-room:test/);
});

test("trusted main restores and publishes every agreed cache", () => {
	for (const cacheName of [
		"pnpm",
		"Turbo",
		"Go",
		"golangci-lint",
		"Cargo",
		"Playwright browser",
		"verified pinned mpv",
	]) {
		assert.match(
			mainWorkflow,
			new RegExp(`Restore ${cacheName}[^\\n]* cache`, "i"),
		);
		assert.match(
			mainWorkflow,
			new RegExp(`Publish ${cacheName}[^\\n]* cache`, "i"),
		);
	}

	for (const saveStep of mainWorkflow.matchAll(
		/- name: Publish [^\n]+ cache[\s\S]*?uses: actions\/cache\/save@[^\n]+/g,
	)) {
		assert.match(saveStep[0], /!cancelled\(\)/);
	}
});

test("pinned mpv cache paths remain runnable and tamper-evident", () => {
	const integrationMpvJob = getJob(integrationWorkflow, "real-mpv");
	const mainDesktopJob = getJob(mainWorkflow, "desktop");

	assert.match(
		integrationMpvJob,
		/Record start time[\s\S]*mkdir -p "\$RUNNER_TEMP\/integration-gate-logs"/,
	);
	assert.match(mainWorkflow, /MPV_CACHE_SCHEMA: v2/);
	assert.match(mainDesktopJob, /bash scripts\/build-pinned-mpv\.sh/);
	assert.match(pinnedMpvBuilder, /binarySha256=.*sha256sum/);
	assert.match(
		pinnedMpvBuilder,
		/echo "binary_sha256=\$\{binarySha256\}" >> "\$\{GITHUB_OUTPUT\}"/,
	);
	assert.match(mainDesktopJob, /name: Verify restored pinned mpv/);
	assert.match(
		mainDesktopJob,
		/name: Verify pinned mpv before cache publication/,
	);
	assert.match(mainDesktopJob, /steps\.mpv_cache_verify\.outcome == 'success'/);
});

test("main builds separate production artifacts and retains diagnostics", () => {
	for (const task of ["web:build", "server:build", "desktop:build"]) {
		assert.match(mainWorkflow, new RegExp(`mise run ${task}`));
	}
	for (const artifact of [
		"main-web-client",
		"main-music-server",
		"main-product-docs",
		"main-desktop-client",
	]) {
		assert.match(mainWorkflow, new RegExp(`name: ${artifact}`));
	}
	assert.match(mainWorkflow, /ARTIFACT_RETENTION_DAYS: 14/);
	assert.doesNotMatch(mainWorkflow, /retention-days: 14/);
	assert.doesNotMatch(mainWorkflow, /14 days|14-day/);
	assert.equal(
		(mainWorkflow.match(/Playwright retry attempts consumed:/g) ?? []).length,
		2,
	);
	for (const cacheName of [
		"pnpm",
		"Turbo",
		"Go",
		"golangci-lint",
		"Playwright",
	]) {
		assert.match(
			getJob(mainWorkflow, "music-server"),
			new RegExp(`${cacheName}=\\$`),
		);
	}
	for (const cacheName of ["pnpm", "Turbo", "Cargo", "verified mpv"]) {
		assert.match(
			getJob(mainWorkflow, "desktop"),
			new RegExp(`${cacheName}=\\$`),
		);
	}
	assert.match(
		getJob(mainWorkflow, "full-verification"),
		/Music Server and Desktop Client are separate artifacts/,
	);
});

test("main consumes committed generated clients and pins external actions", () => {
	assert.doesNotMatch(mainWorkflow, /(?:mise run|pnpm) generate(?:\s|$)/);
	for (const action of mainWorkflow.matchAll(/uses: [^@\n]+@([^\s]+)/g)) {
		assert.match(action[1], /^[a-f0-9]{40}$/);
	}
});

test("nightly verification is scheduled, manually runnable, and never cancelled", () => {
	assert.match(nightlyWorkflow, /name: Nightly/);
	assert.match(nightlyWorkflow, /schedule:\n\s+- cron: "0 3 \* \* \*"/);
	assert.match(nightlyWorkflow, /workflow_dispatch:/);
	assert.match(nightlyWorkflow, /cancel-in-progress: false/);
	assert.doesNotMatch(nightlyWorkflow, /pull_request:|push:/);
	assert.doesNotMatch(nightlyWorkflow, /runs-on: (?!ubuntu-24\.04)/);
});

test("nightly runs full production verification and builds a container", () => {
	assert.match(mainWorkflow, /workflow_call:/);
	assert.match(nightlyWorkflow, /uses: \.\/\.github\/workflows\/main\.yml/);
	assert.match(nightlyWorkflow, /docker build/);
	assert.match(nightlyWorkflow, /docker save/);
	assert.match(nightlyWorkflow, /name: nightly-container/);
});

test("nightly follows trusted cache, summary, and artifact policy", () => {
	for (const cacheName of [
		"pnpm",
		"Turbo",
		"Go",
		"golangci-lint",
		"Cargo",
		"Playwright browser",
		"verified pinned mpv",
	]) {
		assert.match(
			mainWorkflow,
			new RegExp(`Restore ${cacheName}[^\\n]* cache`, "i"),
		);
		assert.match(
			mainWorkflow,
			new RegExp(`Publish ${cacheName}[^\\n]* cache`, "i"),
		);
	}

	assert.match(nightlyWorkflow, /ARTIFACT_RETENTION_DAYS: 14/);
	assert.match(nightlyWorkflow, /Duration:/);
	assert.match(mainWorkflow, /workflow_call:\n\s+outputs:/);
	assert.match(mainWorkflow, /cache_status:/);
	assert.match(mainWorkflow, /retry_count:/);
	assert.match(
		nightlyWorkflow,
		/needs\.full-production-verification\.outputs\.cache_status/,
	);
	assert.match(
		nightlyWorkflow,
		/needs\.full-production-verification\.outputs\.retry_count/,
	);
	assert.match(nightlyWorkflow, /gh api/);
	assert.match(nightlyWorkflow, /archive_download_url/);
	assert.match(nightlyWorkflow, /if ! artifact_links=/);
	assert.match(nightlyWorkflow, /Artifact lookup failed for nightly run/);
	assert.match(nightlyWorkflow, /Run and artifact links:/);
	for (const action of nightlyWorkflow.matchAll(/uses: [^@\n]+@([^\s]+)/g)) {
		assert.match(action[1], /^[a-f0-9]{40}$/);
	}
});
