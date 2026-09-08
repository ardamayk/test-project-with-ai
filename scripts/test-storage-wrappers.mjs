import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import {
	cpSync,
	existsSync,
	mkdirSync,
	mkdtempSync,
	readFileSync,
	rmSync,
	writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

function checked(command, args, options = {}) {
	const result = spawnSync(command, args, { encoding: "utf8", ...options });
	assert.equal(result.status, 0, result.stderr);
	return result.stdout.trim();
}
function fixture(context) {
	const root = mkdtempSync(join(tmpdir(), "earthly-wrappers-"));
	context.after(() => rmSync(root, { recursive: true, force: true }));
	const checkout = join(root, "checkout");
	mkdirSync(join(checkout, "scripts"), { recursive: true });
	for (const name of [
		"storage-env.sh",
		"storage.mjs",
		"storage-paths.mjs",
		"run-with-storage.sh",
		"run-sccache.sh",
		"storage-bin",
	])
		cpSync(new URL(name, import.meta.url), join(checkout, "scripts", name), {
			recursive: true,
		});
	writeFileSync(
		join(checkout, "mise.toml"),
		'[settings]\nactivate_aggressive = true\n[env]\n_.source = { path = "scripts/storage-env.sh", tools = true }\n',
	);
	const bin = join(root, "bin");
	mkdirSync(bin);
	writeFileSync(join(root, "empty.toml"), "");
	for (const command of ["cargo", "go", "pnpm"])
		writeFileSync(
			join(bin, command),
			`#!/usr/bin/env bash
set -euo pipefail
node -e 'const fs=require("fs");const value=JSON.stringify(process.env);if(process.env.SNAPSHOT)fs.writeFileSync(process.env.SNAPSHOT,value);else console.log(value)'
if [[ "\${HOLD:-0}" == 1 ]]; then read -r finish; fi
if [[ -n "\${DAEMON_PID_FILE:-}" ]]; then sleep 30 </dev/null >/dev/null 2>&1 & echo $! > "$DAEMON_PID_FILE"; fi
`,
			{ mode: 0o755 },
		);
	checked("git", ["init", "-q", checkout]);
	checked("git", ["-C", checkout, "add", "."]);
	checked("git", [
		"-C",
		checkout,
		"-c",
		"user.name=Fixture",
		"-c",
		"user.email=fixture@example.test",
		"commit",
		"-qm",
		"fixture",
	]);
	const env = Object.fromEntries(
		Object.entries(process.env).filter(
			([name]) =>
				!name.startsWith("EARTHLY_") &&
				!name.startsWith("MISE_") &&
				!["CARGO_TARGET_DIR", "RUSTC_WRAPPER", "TURBO_CACHE_DIR"].includes(
					name,
				),
		),
	);
	Object.assign(env, {
		PATH: `${bin}:${process.env.PATH}`,
		EARTHLY_STORAGE_MODE: "local",
		EARTHLY_CACHE_ROOT: join(root, "external"),
		XDG_CACHE_HOME: join(root, "xdg"),
		MISE_GLOBAL_CONFIG_FILE: join(root, "empty.toml"),
		MISE_SYSTEM_CONFIG_FILE: join(root, "empty.toml"),
		MISE_AUTO_INSTALL: "false",
		MISE_TRUSTED_CONFIG_PATHS: root,
		MISE_DATA_DIR: join(root, "mise-data"),
		MISE_CACHE_DIR: join(root, "mise-cache"),
		MISE_STATE_DIR: join(root, "mise-state"),
	});
	const options = { cwd: checkout, env };
	return { root, checkout, env, options };
}
function resolve(state, overrides = {}, checkout = state.checkout) {
	return JSON.parse(
		checked(
			"bash",
			[
				"-c",
				'source scripts/storage-env.sh; node -e "console.log(JSON.stringify(process.env))"',
			],
			{ cwd: checkout, env: { ...state.env, ...overrides } },
		),
	);
}
async function waitFile(path) {
	for (let count = 0; !existsSync(path) && count < 150; count++)
		await new Promise((done) => setTimeout(done, 20));
	assert.equal(existsSync(path), true, `Command did not reach ${path}`);
}
function start(state, args, env = {}) {
	const child = spawn("mise", ["exec", "--", ...args], {
		...state.options,
		env: { ...state.env, ...env },
		stdio: ["pipe", "ignore", "inherit"],
	});
	const completed = new Promise((done) => child.once("close", done));
	return {
		child,
		completed,
		async finish() {
			child.stdin.end("finish\n");
			assert.equal(await completed, 0);
		},
	};
}

test("resolver is read-only; real mise exec cargo/go/pnpm receive owned storage", (context) => {
	const state = fixture(context);
	const env = resolve(state);
	assert.equal(existsSync(state.env.EARTHLY_CACHE_ROOT), false);
	for (const command of ["cargo", "go", "pnpm"]) {
		const snapshot = JSON.parse(
			checked("mise", ["exec", "--", command, "test"], state.options),
		);
		assert.equal(snapshot.EARTHLY_LOCKED_WORKTREE, env.EARTHLY_WORKTREE_ROOT);
		assert.equal(snapshot.CARGO_TARGET_DIR, env.CARGO_TARGET_DIR);
		if (command !== "pnpm") {
			assert.equal(
				existsSync(join(snapshot.EARTHLY_RUN_DIR, "run.json")),
				true,
			);
			assert.equal(existsSync(snapshot.EARTHLY_TMP_ALIAS), false);
		}
	}
});

test("parallel raw jobs hold shared locks against cleanup", async (context) => {
	const state = fixture(context);
	const firstFile = join(state.root, "first");
	const secondFile = join(state.root, "second");
	const first = start(state, ["cargo", "test"], {
		HOLD: "1",
		SNAPSHOT: firstFile,
	});
	const second = start(state, ["go", "test"], {
		HOLD: "1",
		SNAPSHOT: secondFile,
	});
	try {
		await Promise.all([waitFile(firstFile), waitFile(secondFile)]);
		const result = spawnSync(
			"mise",
			["exec", "--", "node", "scripts/storage.mjs", "clean", "--apply"],
			state.options,
		);
		assert.notEqual(result.status, 0);
		assert.notEqual(
			JSON.parse(readFileSync(firstFile)).EARTHLY_RUN_DIR,
			JSON.parse(readFileSync(secondFile)).EARTHLY_RUN_DIR,
		);
	} finally {
		await Promise.all([first.finish(), second.finish()]);
	}
});

test("background descendants cannot retain storage lock after wrapper exit", (context) => {
	const state = fixture(context);
	const pidFile = join(state.root, "daemon.pid");
	checked("mise", ["exec", "--", "cargo", "build"], {
		...state.options,
		env: { ...state.env, DAEMON_PID_FILE: pidFile },
	});
	const pid = Number(readFileSync(pidFile, "utf8"));
	try {
		checked(
			"mise",
			["exec", "--", "node", "scripts/storage.mjs", "clean", "--apply"],
			state.options,
		);
	} finally {
		process.kill(pid, "SIGTERM");
	}
});

test("crossing worktrees resets inherited run and defaults while preserving explicit overrides", (context) => {
	const state = fixture(context);
	const initial = resolve(state);
	const linked = join(state.root, "linked");
	checked("git", ["-C", state.checkout, "worktree", "add", "--detach", linked]);
	const inherited = {
		...initial,
		EARTHLY_RUN_DIR: "/old/run",
		EARTHLY_RUN_WORKTREE: initial.EARTHLY_WORKTREE_ROOT,
		EARTHLY_TMP_ALIAS: "/old/tmp",
		TMPDIR: "/old/tmp",
		EARTHLY_PREVIOUS_TMPDIR_SET: "1",
		EARTHLY_PREVIOUS_TMPDIR: "/tmp",
		EARTHLY_WEB_DIST: "/explicit/web",
	};
	const next = resolve(state, inherited, linked);
	assert.notEqual(next.CARGO_TARGET_DIR, initial.CARGO_TARGET_DIR);
	assert.equal(next.TURBO_CACHE_DIR, initial.TURBO_CACHE_DIR);
	assert.equal(next.EARTHLY_RUN_DIR, undefined);
	assert.equal(next.TMPDIR, "/tmp");
	assert.equal(next.EARTHLY_WEB_DIST, "/explicit/web");
});

test("same-worktree frontend artifact builds serialize", async (context) => {
	const state = fixture(context);
	const firstFile = join(state.root, "first");
	const secondFile = join(state.root, "second");
	const args = [
		"bash",
		"scripts/run-with-storage.sh",
		"web-build",
		"pnpm",
		"build",
	];
	const first = start(state, args, { HOLD: "1", SNAPSHOT: firstFile });
	await waitFile(firstFile);
	const second = start(state, args, { HOLD: "1", SNAPSHOT: secondFile });
	try {
		await new Promise((done) => setTimeout(done, 300));
		assert.equal(existsSync(secondFile), false);
		await first.finish();
		await waitFile(secondFile);
	} finally {
		if (first.child.exitCode === null) await first.finish();
		await second.finish();
	}
});

test("compiler server uses persistent temporary storage instead of a test run", (context) => {
	const state = fixture(context);
	const result = checked("bash", ["scripts/run-sccache.sh", "--show-stats"], {
		...state.options,
		env: {
			...state.env,
			EARTHLY_SCCACHE_BINARY: join(state.root, "bin/cargo"),
			TMPDIR: "/expired-test-run",
		},
	});
	const snapshot = JSON.parse(result);
	assert.equal(
		snapshot.TMPDIR,
		join(state.env.XDG_CACHE_HOME, "earthly-audio/sccache-tmp"),
	);
	assert.equal(existsSync(snapshot.TMPDIR), true);
});

test("only local frontend builds disable Turbo result caching", (context) => {
	const state = fixture(context);
	const script = new URL("run-workspace-task.sh", import.meta.url).pathname;
	writeFileSync(
		join(state.root, "bin/pnpm"),
		'#!/usr/bin/env bash\nprintf "%s\\n" "$@"\n',
		{ mode: 0o755 },
	);
	for (const mode of ["local", "ci", "clean-room", "legacy"]) {
		for (const task of ["build", "test:unit"]) {
			const output = checked("bash", [script, task, "web"], {
				...state.options,
				env: { ...state.env, EARTHLY_STORAGE_RESOLVED_MODE: mode },
			});
			assert.equal(
				output.split("\n").includes("--cache="),
				mode === "local" && task === "build",
			);
		}
	}
});

test("unchanged Go tests reuse results despite separate invocation runs", (context) => {
	const state = fixture(context);
	const go =
		process.env.EARTHLY_REAL_GO || checked("bash", ["-c", "command -v go"]);
	writeFileSync(
		join(state.root, "bin/go"),
		`#!/usr/bin/env bash\nexec '${go.replaceAll("'", "'\\''")}' "$@"\n`,
		{ mode: 0o755 },
	);
	writeFileSync(
		join(state.checkout, "go.mod"),
		"module storage-cache-test\ngo 1.24\n",
	);
	writeFileSync(
		join(state.checkout, "storage_test.go"),
		'package storage\nimport "testing"\nfunc TestTemporaryDirectory(t *testing.T) { t.TempDir() }\n',
	);
	const args = ["exec", "--", "go", "test", "./..."];
	checked("mise", args, state.options);
	const result = checked("mise", args, state.options);
	assert.match(result, /\(cached\)/);
});

test("direct Playwright configuration initializes ownership before fallback runs", (context) => {
	const state = fixture(context);
	const directory = join(state.checkout, "web/testing");
	mkdirSync(directory, { recursive: true });
	cpSync(
		new URL("../web/testing/storage.ts", import.meta.url),
		join(directory, "storage.ts"),
	);
	const runDirectory = checked(
		"mise",
		[
			"exec",
			"--",
			"node",
			"--input-type=module",
			"-e",
			'import { getTestRunDirectory } from "./web/testing/storage.ts"; console.log(getTestRunDirectory());',
		],
		state.options,
	);
	const env = resolve(state);
	assert.equal(existsSync(join(env.EARTHLY_CLONE_ROOT, "owner.json")), true);
	assert.equal(existsSync(join(env.EARTHLY_WORKTREE_ROOT, "owner.json")), true);
	assert.equal(existsSync(join(runDirectory, "run.json")), true);
	checked("mise", ["exec", "--", "cargo", "build"], state.options);
	checked(
		"mise",
		["exec", "--", "node", "scripts/storage.mjs", "clean", "--apply"],
		state.options,
	);
	assert.equal(existsSync(runDirectory), false);
});

test("surviving descendants reacquire storage locks before running tools", async (context) => {
	const state = fixture(context);
	const snapshot = join(state.root, "snapshot");
	const begin = join(state.root, "begin");
	const finish = join(state.root, "finish");
	const done = join(state.root, "done");
	writeFileSync(
		join(state.root, "bin/cargo"),
		'#!/usr/bin/env bash\nnode -e \'require("node:fs").writeFileSync(process.env.SNAPSHOT, "ready")\'\nwhile [[ ! -f "$FINISH" ]]; do sleep 0.02; done\n',
		{ mode: 0o755 },
	);
	checked(
		"mise",
		[
			"exec",
			"--",
			"bash",
			"scripts/run-with-storage.sh",
			"build",
			"bash",
			"-c",
			'(while [[ ! -f "$BEGIN" ]]; do sleep 0.02; done; cargo build; touch "$DONE") </dev/null >"$LOG" 2>&1 &',
		],
		{
			...state.options,
			env: {
				...state.env,
				SNAPSHOT: snapshot,
				BEGIN: begin,
				FINISH: finish,
				DONE: done,
				LOG: join(state.root, "background.log"),
			},
		},
	);
	try {
		checked(
			"mise",
			["exec", "--", "node", "scripts/storage.mjs", "clean", "--apply"],
			state.options,
		);
		writeFileSync(begin, "start");
		await waitFile(snapshot);
		const result = spawnSync(
			"mise",
			["exec", "--", "node", "scripts/storage.mjs", "clean", "--apply"],
			state.options,
		);
		assert.notEqual(
			result.status,
			0,
			"The descendant must hold a fresh shared lock",
		);
	} finally {
		writeFileSync(begin, "start");
		writeFileSync(finish, "finish");
		await waitFile(done);
	}
	checked(
		"mise",
		["exec", "--", "node", "scripts/storage.mjs", "clean", "--apply"],
		state.options,
	);
});
