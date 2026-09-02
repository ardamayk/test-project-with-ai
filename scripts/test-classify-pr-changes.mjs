import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
	mkdirSync,
	mkdtempSync,
	renameSync,
	rmSync,
	writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

const CLASSIFIER_PATH = new URL("classify-pr-changes.mjs", import.meta.url);
const TEST_ENVIRONMENT = { ...process.env };
for (const variableName of [
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_COMMON_DIR",
	"GIT_DIR",
	"GIT_INDEX_FILE",
	"GIT_OBJECT_DIRECTORY",
	"GIT_PREFIX",
	"GIT_WORK_TREE",
]) {
	delete TEST_ENVIRONMENT[variableName];
}

function run(command, args, cwd) {
	const result = spawnSync(command, args, {
		cwd,
		encoding: "utf8",
		env: TEST_ENVIRONMENT,
	});
	assert.equal(
		result.status,
		0,
		`${command} ${args.join(" ")} failed:\n${result.stderr}${result.stdout}`,
	);
	return result.stdout;
}

function writeFixtureFile(repositoryPath, filePath, content) {
	const absolutePath = join(repositoryPath, filePath);
	mkdirSync(join(absolutePath, ".."), { recursive: true });
	writeFileSync(absolutePath, content);
}

const EMPTY_SELECTION = {
	workspace_fast: true,
	server_fast: true,
	desktop_unit: false,
	generated_drift: false,
	web_e2e: false,
	hls: false,
	real_mpv: false,
	integration_required: false,
	reasons: [],
};

const ALL_SELECTION = {
	workspace_fast: true,
	server_fast: true,
	desktop_unit: true,
	generated_drift: true,
	web_e2e: true,
	hls: true,
	real_mpv: true,
	integration_required: true,
};

function currentRevision(repositoryPath) {
	return run("git", ["rev-parse", "HEAD"], repositoryPath).trim();
}

function initializeFixtureRepository(repositoryPath, changes) {
	run("git", ["init", "--quiet"], repositoryPath);
	run("git", ["config", "user.email", "test@example.com"], repositoryPath);
	run("git", ["config", "user.name", "Test User"], repositoryPath);
	writeFixtureFile(repositoryPath, ".fixture-base", "base\n");
	for (const change of changes) {
		if (change.type !== "add") {
			writeFixtureFile(repositoryPath, change.path, "base\n");
		}
	}
	run("git", ["add", "."], repositoryPath);
	run("git", ["commit", "--quiet", "-m", "base"], repositoryPath);
	return currentRevision(repositoryPath);
}

function applyFixtureChange(repositoryPath, change) {
	if (change.type === "add" || change.type === "modify") {
		writeFixtureFile(repositoryPath, change.path, "head\n");
	} else if (change.type === "delete") {
		rmSync(join(repositoryPath, change.path));
	} else if (change.type === "rename") {
		const destinationPath = join(repositoryPath, change.destination);
		mkdirSync(join(destinationPath, ".."), { recursive: true });
		renameSync(join(repositoryPath, change.path), destinationPath);
	} else {
		throw new Error(`Unsupported fixture change: ${change.type}`);
	}
}

function createHeadRevision(repositoryPath, changes) {
	for (const change of changes) {
		applyFixtureChange(repositoryPath, change);
	}
	run("git", ["add", "--all"], repositoryPath);
	run("git", ["commit", "--quiet", "-m", "head"], repositoryPath);
	return currentRevision(repositoryPath);
}

function runClassifier(repositoryPath, baseRevision, headRevision, format) {
	return run(
		process.execPath,
		[
			CLASSIFIER_PATH.pathname,
			"--base",
			baseRevision,
			"--head",
			headRevision,
			"--format",
			format,
		],
		repositoryPath,
	);
}

function classifyChanges(changes, format = "json") {
	const repositoryPath = mkdtempSync(join(tmpdir(), "pr-classifier-test-"));
	try {
		const baseRevision = initializeFixtureRepository(repositoryPath, changes);
		const headRevision = createHeadRevision(repositoryPath, changes);
		const output = runClassifier(
			repositoryPath,
			baseRevision,
			headRevision,
			format,
		);
		return format === "json" ? JSON.parse(output) : output;
	} finally {
		rmSync(repositoryPath, { force: true, recursive: true });
	}
}

const PATH_POLICY_FIXTURES = [
	{
		name: "Web Client",
		changes: [{ type: "modify", path: "web/src/routes/index.tsx" }],
		expected: {
			...EMPTY_SELECTION,
			web_e2e: true,
			integration_required: true,
			reasons: ["web"],
		},
	},
	{
		name: "shared UI",
		changes: [{ type: "modify", path: "packages/ui/src/AppShell.tsx" }],
		expected: {
			...EMPTY_SELECTION,
			web_e2e: true,
			integration_required: true,
			reasons: ["web"],
		},
	},
	{
		name: "API client",
		changes: [{ type: "modify", path: "packages/api-client/src/index.ts" }],
		expected: {
			...EMPTY_SELECTION,
			web_e2e: true,
			integration_required: true,
			reasons: ["web"],
		},
	},
	{
		name: "Music Server",
		changes: [{ type: "modify", path: "server/internal/api/router.go" }],
		expected: {
			...EMPTY_SELECTION,
			web_e2e: true,
			hls: true,
			integration_required: true,
			reasons: ["server"],
		},
	},
	{
		name: "Desktop Client",
		changes: [{ type: "modify", path: "desktop/src-tauri/src/main.rs" }],
		expected: {
			...EMPTY_SELECTION,
			desktop_unit: true,
			real_mpv: true,
			integration_required: true,
			reasons: ["desktop"],
		},
	},
	{
		name: "API contract",
		changes: [{ type: "modify", path: "packages/contracts/openapi.yaml" }],
		expected: { ...ALL_SELECTION, reasons: ["contract"] },
	},
	{
		name: "global lockfile",
		changes: [{ type: "modify", path: "pnpm-lock.yaml" }],
		expected: { ...ALL_SELECTION, reasons: ["global"] },
	},
	...[
		["global configuration", "biome.json"],
		["toolchain configuration", "mise.toml"],
		["workspace configuration", "pnpm-workspace.yaml"],
		["workflow configuration", ".github/workflows/ci.yml"],
		["build-system configuration", "turbo.json"],
	].map(([name, path]) => ({
		name,
		changes: [{ type: "modify", path }],
		expected: { ...ALL_SELECTION, reasons: ["global"] },
	})),
	{
		name: "code-generation input",
		changes: [{ type: "modify", path: "scripts/generate-openapi.sh" }],
		expected: { ...ALL_SELECTION, reasons: ["contract"] },
	},
	{
		name: "documentation only",
		changes: [
			{ type: "modify", path: "docs/testing/guide.md" },
			{ type: "modify", path: "packages/docs/content/guide.mdx" },
		],
		expected: { ...EMPTY_SELECTION, reasons: ["documentation"] },
	},
	{
		name: "unknown path",
		changes: [{ type: "add", path: "unowned/tool.config" }],
		expected: { ...ALL_SELECTION, reasons: ["unknown"] },
	},
	{
		name: "mixed Web and Desktop changes",
		changes: [
			{ type: "modify", path: "web/src/main.tsx" },
			{ type: "modify", path: "desktop/src/main.ts" },
		],
		expected: {
			...EMPTY_SELECTION,
			desktop_unit: true,
			web_e2e: true,
			real_mpv: true,
			integration_required: true,
			reasons: ["desktop", "web"],
		},
	},
	{
		name: "deleted server file",
		changes: [{ type: "delete", path: "server/obsolete.go" }],
		expected: {
			...EMPTY_SELECTION,
			web_e2e: true,
			hls: true,
			integration_required: true,
			reasons: ["server"],
		},
	},
	{
		name: "server file renamed into documentation",
		changes: [
			{
				type: "rename",
				path: "server/obsolete.go",
				destination: "docs/obsolete.md",
			},
		],
		expected: {
			...EMPTY_SELECTION,
			web_e2e: true,
			hls: true,
			integration_required: true,
			reasons: ["documentation", "server"],
		},
	},
];

test("path policy selects conservative validation", async (testContext) => {
	for (const fixture of PATH_POLICY_FIXTURES) {
		await testContext.test(fixture.name, () => {
			assert.deepEqual(classifyChanges(fixture.changes), fixture.expected);
		});
	}
});

test("GitHub output format publishes every gate input", () => {
	const output = classifyChanges(
		[{ type: "modify", path: "desktop/src-tauri/src/main.rs" }],
		"github",
	);
	assert.deepEqual(
		Object.fromEntries(
			output
				.trim()
				.split("\n")
				.map((line) => line.split("=")),
		),
		{
			workspace_fast: "true",
			server_fast: "true",
			desktop_unit: "true",
			generated_drift: "false",
			web_e2e: "false",
			hls: "false",
			real_mpv: "true",
			integration_required: "true",
			reasons: '["desktop"]',
		},
	);
});
