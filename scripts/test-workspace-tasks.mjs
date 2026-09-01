import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import test from "node:test";

const WORKSPACE_PACKAGES = ["@repo/api-client", "@repo/ui", "web"];

function run(command, args) {
	const result = spawnSync(command, args, {
		cwd: new URL("..", import.meta.url),
		encoding: "utf8",
		env: { ...process.env, NO_COLOR: "1" },
	});

	assert.equal(
		result.status,
		0,
		`${command} ${args.join(" ")} failed:\n${result.stderr}${result.stdout}`,
	);

	return result.stdout;
}

function parseDryRun(output) {
	const jsonStart = output.indexOf("{");
	assert.notEqual(jsonStart, -1, `Turbo JSON missing:\n${output}`);
	return JSON.parse(output.slice(jsonStart));
}

function runMiseDry(task) {
	return parseDryRun(run("mise", ["run", task, "--", "--dry=json"]));
}

function runMiseTaskDryRun(task) {
	const result = spawnSync("mise", ["run", "--dry-run", task], {
		cwd: new URL("..", import.meta.url),
		encoding: "utf8",
		env: { ...process.env, NO_COLOR: "1" },
	});

	assert.equal(
		result.status,
		0,
		`mise run --dry-run ${task} failed:\n${result.stderr}${result.stdout}`,
	);

	return `${result.stderr}${result.stdout}`;
}

function readMiseTasks() {
	return JSON.parse(run("mise", ["tasks", "--json"]));
}

function runTurboDry(task, packageName = "web") {
	return parseDryRun(
		run("pnpm", [
			"exec",
			"turbo",
			"run",
			task,
			`--filter=${packageName}`,
			"--dry=json",
		]),
	);
}

function taskPackages(dryRun, taskName) {
	return dryRun.tasks
		.filter(
			(task) => task.task === taskName && task.command !== "<NONEXISTENT>",
		)
		.map((task) => task.package)
		.sort();
}

test("aggregate Mise tasks select only workspace verification packages", () => {
	assert.deepEqual(taskPackages(runMiseDry("workspace:build"), "build"), [
		"web",
	]);

	for (const [miseTask, turboTask] of [
		["workspace:format", "format"],
		["workspace:check", "check"],
		["workspace:typecheck", "typecheck"],
		["workspace:test", "test:unit"],
	]) {
		assert.deepEqual(
			taskPackages(runMiseDry(miseTask), turboTask),
			WORKSPACE_PACKAGES,
		);
	}
});

test("targeted Mise tasks select one workspace package", () => {
	const packageTasks = [
		["web", "web"],
		["ui", "@repo/ui"],
		["api-client", "@repo/api-client"],
	];

	assert.match(runMiseTaskDryRun("web:build"), /\[workspace:build\]/);

	for (const [taskPrefix, packageName] of packageTasks) {
		for (const [taskSuffix, turboTask] of [
			["format", "format"],
			["check", "check"],
			["typecheck", "typecheck"],
			["test", "test:unit"],
		]) {
			assert.deepEqual(
				taskPackages(runMiseDry(`${taskPrefix}:${taskSuffix}`), turboTask),
				[packageName],
			);
		}
	}
});

test("Mise exposes the cross-language public task contract", () => {
	const taskNames = new Set(readMiseTasks().map((task) => task.name));

	for (const taskName of [
		"dev",
		"web:dev",
		"desktop:dev",
		"build",
		"web:build",
		"docs:build",
		"server:build",
		"desktop:build",
		"format",
		"server:format",
		"desktop:format",
		"check",
		"server:check",
		"desktop:check",
		"test",
		"server:test",
		"desktop:test",
		"generate:check",
		"ci:fast",
		"ci:integration",
		"ci:full",
	]) {
		assert.equal(taskNames.has(taskName), true, taskName);
	}
});

test("aggregate Mise tasks select every language domain", () => {
	for (const [aggregateTask, selectedTasks] of [
		[
			"build",
			["web:build", "docs:build", "server:build", "desktop:build"],
		],
		["format", ["workspace:format", "server:format", "desktop:format"]],
		["check", ["workspace:check", "server:check", "desktop:check"]],
		["test", ["workspace:test", "server:test", "desktop:test"]],
	]) {
		const dryRun = runMiseTaskDryRun(aggregateTask);
		for (const selectedTask of selectedTasks) {
			assert.match(dryRun, new RegExp(`\\[${selectedTask}\\]`), aggregateTask);
		}
	}
});

test("targeted native Mise tasks select their domain commands", () => {
	for (const [taskName, expectedCommands] of [
		["server:build", ["web:build", "docs:build", "[server:build]"]],
		["server:check", ["server:format:check", "server:lint"]],
		["server:test", ["go test ./..."]],
		["desktop:build", ["web:build", "build:sidecar:prepared"]],
		["desktop:check", ["desktop:format:check", "desktop:lint"]],
		["desktop:test", ["test:unit"]],
		["web:dev", ["run-with-music-server.sh", "web dev"]],
		["desktop:dev", ["run-with-music-server.sh", "desktop:dev"]],
	]) {
		const dryRun = runMiseTaskDryRun(taskName);
		for (const expectedCommand of expectedCommands) {
			assert.equal(dryRun.includes(expectedCommand), true, taskName);
		}
	}
});

test("aggregate Mise tasks propagate dependency failures", () => {
	const probeDirectory = mkdtempSync(
		new URL("../server/.mise-failure-probe-", import.meta.url),
	);
	const probePath = `${probeDirectory}/probe.go`;
	writeFileSync(probePath, "package server\n\nfunc miseFailureProbe( ){ }\n");

	try {
		const result = spawnSync("mise", ["run", "format:check"], {
			cwd: new URL("..", import.meta.url),
			encoding: "utf8",
			env: { ...process.env, NO_COLOR: "1" },
		});

		assert.notEqual(result.status, 0);
		assert.match(`${result.stderr}${result.stdout}`, /server:format:check/);
	} finally {
		rmSync(probeDirectory, { force: true, recursive: true });
	}
});

test("CI policy tasks reuse public task compositions", () => {
	const fastDryRun = runMiseTaskDryRun("ci:fast");
	assert.match(fastDryRun, /\[check\]/);
	assert.match(fastDryRun, /\[test\]/);

	const integrationDryRun = runMiseTaskDryRun("ci:integration");
	assert.match(integrationDryRun, /\[web:test:e2e\]/);
	assert.match(integrationDryRun, /\[server:test:hls\]/);
	assert.match(integrationDryRun, /\[desktop:test:mpv\]/);

	const fullDryRun = runMiseTaskDryRun("ci:full");
	assert.match(fullDryRun, /\[ci:fast\]/);
	const fullTask = readMiseTasks().find((task) => task.name === "ci:full");
	const fullRun = fullTask.run.join("\n");
	const generateIndex = fullRun.indexOf("mise run generate:check");
	const integrationIndex = fullRun.indexOf("mise run ci:integration");
	const buildIndex = fullRun.indexOf("mise run build");
	assert.notEqual(generateIndex, -1);
	assert.equal(generateIndex < integrationIndex, true);
	assert.equal(integrationIndex < buildIndex, true);

	const generateCheck = readMiseTasks().find(
		(task) => task.name === "generate:check",
	);
	assert.match(generateCheck.run.join("\n"), /mise run generate/);
	assert.match(generateCheck.run.join("\n"), /git diff --exit-code/);
});

test("root pnpm compatibility commands delegate to Mise", async () => {
	const packageJson = await import("../package.json", { with: { type: "json" } });

	for (const scriptName of [
		"build",
		"dev",
		"format",
		"format:check",
		"lint",
		"typecheck",
		"test",
		"test:unit",
		"test:core",
		"test:desktop",
		"test:e2e",
		"test:e2e:hls",
		"generate",
		"check",
		"start",
	]) {
		assert.match(packageJson.default.scripts[scriptName], /^mise run /, scriptName);
	}
});

test("Desktop Client workspace scripts expose native Rust tools", async () => {
	const packageJson = await import("../desktop/package.json", {
		with: { type: "json" },
	});

	assert.match(packageJson.default.scripts.format, /^cargo fmt /);
	assert.match(packageJson.default.scripts["format:check"], /^cargo fmt /);
	assert.match(packageJson.default.scripts.lint, /^cargo clippy /);
	assert.match(packageJson.default.scripts.lint, /-- -D warnings$/);
	assert.match(packageJson.default.scripts["test:unit"], /^cargo test /);
});

test("Turbo verification tasks have no implicit build or generation edges", () => {
	const buildTaskIds = runTurboDry("build").tasks.map((task) => task.taskId);
	assert.equal(
		buildTaskIds.some((taskId) => taskId.endsWith("#generate")),
		false,
	);

	for (const taskName of ["lint", "test:unit"]) {
		const taskIds = runTurboDry(taskName).tasks.map((task) => task.taskId);
		assert.equal(
			taskIds.some((taskId) => taskId.endsWith("#build")),
			false,
		);
		assert.equal(
			taskIds.some((taskId) => taskId.endsWith("#typecheck")),
			false,
		);
	}
});

test("Turbo cache policy matches task side effects", () => {
	for (const taskName of [
		"build",
		"format:check",
		"lint",
		"typecheck",
		"test:unit",
	]) {
		const [task] = runTurboDry(taskName).tasks;
		assert.equal(task.resolvedTaskDefinition.cache, true, taskName);
	}

	for (const [taskName, packageName] of [
		["format", "web"],
		["generate", "@repo/contracts"],
		["test:e2e", "web"],
		["test:integration", "web"],
	]) {
		const [task] = runTurboDry(taskName, packageName).tasks;
		assert.notEqual(task.command, "<NONEXISTENT>", taskName);
		assert.equal(task.resolvedTaskDefinition.cache, false, taskName);
	}

	const [devTask] = runTurboDry("dev").tasks;
	assert.equal(devTask.resolvedTaskDefinition.cache, false);
	assert.equal(devTask.resolvedTaskDefinition.persistent, true);
});

test("workspace Biome policy excludes generated static bundles", () => {
	run("pnpm", [
		"exec",
		"biome",
		"check",
		"--no-errors-on-unmatched",
		"server/internal/staticassets/web",
	]);
});

test("compiler-only packages expose type checking instead of building", async () => {
	for (const packagePath of [
		"../packages/ui/package.json",
		"../packages/api-client/package.json",
	]) {
		const packageJson = await import(packagePath, { with: { type: "json" } });
		assert.equal(packageJson.default.scripts.build, undefined);
		assert.equal(packageJson.default.scripts.typecheck, "tsc --noEmit");
	}
});
