import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
	existsSync,
	mkdirSync,
	mkdtempSync,
	readFileSync,
	rmSync,
	writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { delimiter, join } from "node:path";
import test from "node:test";

const repositoryRoot = new URL("..", import.meta.url);
const runnerPath = new URL("run-clean-room-verification.sh", import.meta.url);
const cleanRoomWorkflow = readFileSync(
	new URL("../.github/workflows/clean-room.yml", import.meta.url),
	"utf8",
);

function createCommandShim(binDirectory, commandName) {
	const commandPath = join(binDirectory, commandName);
	writeFileSync(
		commandPath,
		`#!/usr/bin/env bash
set -euo pipefail
printf '%s\\t%s\\t%s\\t%s\\t%s\\t%s\\t%s\\t%s\\t%s\\t%s\\t%s\\t%s\\t%s\\n' \\
  "${commandName}" "$*" "$GOCACHE" "$GOMODCACHE" "$CARGO_HOME" \\
  "$CARGO_TARGET_DIR" "$TURBO_CACHE_DIR" "$PLAYWRIGHT_BROWSERS_PATH" \\
  "$XDG_CACHE_HOME" "$MISE_CACHE_DIR" "$MISE_DATA_DIR" "$MISE_STATE_DIR" \\
  "$BUILDX_CONFIG" >> "$CLEAN_ROOM_COMMAND_LOG"
if [[ "$TURBO_CACHE" != "local:w" ]]; then
  exit 24
fi
if [[ "${commandName} $*" == *"\${CLEAN_ROOM_FAIL_COMMAND:-never-match}"* ]]; then
  exit 23
fi
`,
		{ mode: 0o755 },
	);
}

function createFixture() {
	const testDirectory = mkdtempSync(join(tmpdir(), "clean-room-test-"));
	const binDirectory = join(testDirectory, "bin");
	const tempParent = join(testDirectory, "temporary");
	const developerCache = join(testDirectory, "developer-cache");
	const commandLog = join(testDirectory, "commands.log");
	mkdirSync(binDirectory);
	mkdirSync(tempParent);
	mkdirSync(developerCache);
	writeFileSync(join(developerCache, "sentinel"), "preserve");

	for (const commandName of ["pnpm", "mise", "docker"]) {
		createCommandShim(binDirectory, commandName);
	}
	return {
		binDirectory,
		commandLog,
		developerCache,
		tempParent,
		testDirectory,
	};
}

function runCleanRoom({ failCommand } = {}) {
	const fixture = createFixture();
	const result = spawnSync("bash", [runnerPath.pathname], {
		cwd: repositoryRoot,
		encoding: "utf8",
		env: {
			...process.env,
			PATH: `${fixture.binDirectory}${delimiter}${process.env.PATH}`,
			TMPDIR: fixture.tempParent,
			CLEAN_ROOM_COMMAND_LOG: fixture.commandLog,
			CLEAN_ROOM_FAIL_COMMAND: failCommand ?? "",
			GOCACHE: fixture.developerCache,
			GOMODCACHE: fixture.developerCache,
			CARGO_HOME: fixture.developerCache,
			CARGO_TARGET_DIR: fixture.developerCache,
			TURBO_CACHE_DIR: fixture.developerCache,
			PLAYWRIGHT_BROWSERS_PATH: fixture.developerCache,
			XDG_CACHE_HOME: fixture.developerCache,
			MISE_CACHE_DIR: fixture.developerCache,
			MISE_DATA_DIR: fixture.developerCache,
			MISE_STATE_DIR: fixture.developerCache,
			BUILDX_CONFIG: fixture.developerCache,
		},
	});

	return { ...fixture, result };
}

function assertIsolatedPaths(commandLog, tempParent) {
	const rows = readFileSync(commandLog, "utf8").trim().split("\n");
	for (const row of rows) {
		const cachePaths = row.split("\t").slice(2);
		for (const cachePath of cachePaths) {
			assert.match(cachePath, new RegExp(`^${tempParent}/clean-room\\.`));
			assert.equal(existsSync(cachePath.split("/cache/")[0]), false);
		}
	}
	return rows;
}

test("local clean-room run isolates caches, forces verification, and cleans up", () => {
	const fixture = runCleanRoom();
	try {
		assert.equal(
			fixture.result.status,
			0,
			`${fixture.result.stderr}${fixture.result.stdout}`,
		);
		const rows = assertIsolatedPaths(fixture.commandLog, fixture.tempParent);
		assert.match(
			rows[0],
			/^pnpm\tinstall --frozen-lockfile --force --ignore-scripts --store-dir /,
		);
		assert.match(
			rows[1],
			/^pnpm\t--filter web exec playwright install chromium --with-deps/,
		);
		assert.match(rows[2], /^mise\trun --force --task-cache=off ci:full\t/);
		assert.match(
			rows[3],
			/^docker\tbuildx create --name navidrome-clean-room-/,
		);
		assert.match(
			rows[4],
			/^docker\tbuildx build --builder navidrome-clean-room-.* --pull --no-cache --output type=oci,dest=.*\/navidrome-replacement\.tar \.\t/,
		);
		assert.match(rows[5], /^docker\tbuildx rm --force navidrome-clean-room-/);
		assert.equal(
			readFileSync(join(fixture.developerCache, "sentinel"), "utf8"),
			"preserve",
		);
		assert.deepEqual(
			readFileSync(fixture.commandLog, "utf8").match(
				/(?:^|\t)generate(?:\s|$)/gm,
			),
			null,
		);
	} finally {
		rmSync(fixture.testDirectory, { force: true, recursive: true });
	}
});

test("local clean-room run propagates failure and still cleans up", () => {
	const fixture = runCleanRoom({ failCommand: "mise" });
	try {
		assert.equal(fixture.result.status, 23);
		assertIsolatedPaths(fixture.commandLog, fixture.tempParent);
		assert.equal(
			readFileSync(join(fixture.developerCache, "sentinel"), "utf8"),
			"preserve",
		);
	} finally {
		rmSync(fixture.testDirectory, { force: true, recursive: true });
	}
});

test("local clean-room run removes disposable builder after build failure", () => {
	const fixture = runCleanRoom({ failCommand: "docker buildx build" });
	try {
		assert.equal(fixture.result.status, 23);
		const rows = assertIsolatedPaths(fixture.commandLog, fixture.tempParent);
		assert.match(
			rows.at(-1),
			/^docker\tbuildx rm --force navidrome-clean-room-/,
		);
	} finally {
		rmSync(fixture.testDirectory, { force: true, recursive: true });
	}
});

test("weekly workflow runs same clean-room contract without GitHub caches", () => {
	assert.match(cleanRoomWorkflow, /schedule:\n\s+- cron: "[^\n]+"/);
	assert.match(cleanRoomWorkflow, /workflow_dispatch:/);
	assert.equal(
		cleanRoomWorkflow.indexOf("- name: Record start time") <
			cleanRoomWorkflow.indexOf("- name: Check out repository"),
		true,
	);
	assert.match(cleanRoomWorkflow, /clean-room-logs\/run-metadata\.txt/);
	assert.match(
		cleanRoomWorkflow,
		/bash scripts\/run-clean-room-verification\.sh/,
	);
	assert.match(cleanRoomWorkflow, /cache: false/);
	assert.doesNotMatch(cleanRoomWorkflow, /actions\/cache\//);
	assert.doesNotMatch(cleanRoomWorkflow, /cache_save:/);
	assert.match(
		cleanRoomWorkflow,
		/if: failure\(\)[\s\S]*actions\/upload-artifact@/,
	);
	assert.match(cleanRoomWorkflow, /if-no-files-found: error/);
	assert.match(cleanRoomWorkflow, /if: always\(\)[\s\S]*GITHUB_STEP_SUMMARY/);
});

test("container build consumes committed clients without regeneration", () => {
	const dockerfile = readFileSync(
		new URL("../Dockerfile", import.meta.url),
		"utf8",
	);
	assert.doesNotMatch(dockerfile, /turbo run generate|generate-openapi/);
	assert.match(dockerfile, /turbo run build --filter=web --filter=@repo\/docs/);
});
