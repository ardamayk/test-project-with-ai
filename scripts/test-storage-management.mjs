import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import {
	cpSync,
	existsSync,
	mkdirSync,
	mkdtempSync,
	readdirSync,
	readFileSync,
	rmSync,
	symlinkSync,
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
	const root = mkdtempSync(join(tmpdir(), "earthly-management-"));
	context.after(() => rmSync(root, { recursive: true, force: true }));
	const checkout = join(root, "checkout with spaces");
	mkdirSync(join(checkout, "scripts"), { recursive: true });
	for (const name of [
		"storage-env.sh",
		"storage.mjs",
		"storage-paths.mjs",
		"run-with-storage.sh",
	]) {
		cpSync(new URL(name, import.meta.url), join(checkout, "scripts", name));
	}
	checked("git", ["init", "-q", checkout]);
	checked("git", ["-C", checkout, "add", "scripts"]);
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
				!["CARGO_TARGET_DIR", "RUSTC_WRAPPER", "TURBO_CACHE_DIR"].includes(
					name,
				),
		),
	);
	Object.assign(env, {
		EARTHLY_STORAGE_MODE: "local",
		EARTHLY_CACHE_ROOT: join(root, "external"),
		XDG_CACHE_HOME: join(root, "xdg"),
	});
	const invoke = (args, overrides = {}, directory = checkout) =>
		spawnSync(
			"bash",
			[
				"-c",
				'source scripts/storage-env.sh; node scripts/storage.mjs "$@"',
				"storage",
				...args,
			],
			{ cwd: directory, env: { ...env, ...overrides }, encoding: "utf8" },
		);
	const resolved = JSON.parse(
		checked(
			"bash",
			[
				"-c",
				'source scripts/storage-env.sh; node -e "console.log(JSON.stringify(process.env))"',
			],
			{ cwd: checkout, env },
		),
	);
	return { root, checkout, env, invoke, resolved };
}

function put(path, value = "sentinel") {
	mkdirSync(join(path, ".."), { recursive: true });
	writeFileSync(path, value);
}

function successful(result) {
	assert.equal(result.status, 0, `${result.stderr}${result.stdout}`);
}

test("management previews leave new storage absent and existing source unchanged", (context) => {
	const state = fixture(context);
	const target = join(state.checkout, "desktop/src-tauri/target/object");
	put(target);
	for (const action of ["status", "migrate", "prune", "clean"])
		successful(state.invoke([action]));
	assert.equal(existsSync(state.env.EARTHLY_CACHE_ROOT), false);
	assert.equal(readFileSync(target, "utf8"), "sentinel");
	assert.equal(existsSync(state.env.XDG_CACHE_HOME), false);
});

test("migration moves generated target and preserves incremental files", (context) => {
	const state = fixture(context);
	const source = join(state.checkout, "desktop/src-tauri/target");
	put(join(source, "debug/incremental/object"));
	successful(state.invoke(["migrate", "--apply"]));
	assert.equal(existsSync(source), false);
	assert.equal(
		readFileSync(
			join(state.resolved.CARGO_TARGET_DIR, "debug/incremental/object"),
			"utf8",
		),
		"sentinel",
	);
});

test("migration skips custom and occupied destinations", (context) => {
	const state = fixture(context);
	const source = join(state.checkout, "desktop/src-tauri/target/object");
	put(source);
	const custom = join(state.root, "custom-target");
	successful(
		state.invoke(["migrate", "--apply"], { CARGO_TARGET_DIR: custom }),
	);
	assert.equal(existsSync(custom), false);
	successful(state.invoke(["prepare"]));
	put(join(state.resolved.CARGO_TARGET_DIR, "existing"));
	successful(state.invoke(["migrate", "--apply"]));
	assert.equal(readFileSync(source, "utf8"), "sentinel");
	assert.equal(
		readFileSync(join(state.resolved.CARGO_TARGET_DIR, "existing"), "utf8"),
		"sentinel",
	);
});

test("migration rejects tracked files and symbolic source paths", (context) => {
	const state = fixture(context);
	const source = join(state.checkout, "desktop/src-tauri/target");
	put(join(source, "tracked"));
	checked("git", ["-C", state.checkout, "add", "desktop"]);
	assert.notEqual(state.invoke(["migrate", "--apply"]).status, 0);
	checked("git", ["-C", state.checkout, "rm", "-r", "--cached", "desktop"]);
	rmSync(source, { recursive: true });
	const outside = join(state.root, "outside");
	put(join(outside, "preserve"));
	symlinkSync(outside, source);
	assert.notEqual(state.invoke(["migrate", "--apply"]).status, 0);
	assert.equal(readFileSync(join(outside, "preserve"), "utf8"), "sentinel");
});

test("clean preserves shared cache and real music; dry clean preserves outputs", (context) => {
	const state = fixture(context);
	successful(state.invoke(["lock-path"]));
	successful(state.invoke(["prepare"]));
	const output = join(state.resolved.CARGO_TARGET_DIR, "object");
	const shared = join(state.resolved.TURBO_CACHE_DIR, "cache-entry");
	const music = join(state.checkout, "server/data/music.flac");
	put(output);
	put(shared);
	put(music);
	const owner = join(state.resolved.EARTHLY_WORKTREE_ROOT, "owner.json");
	const before = readFileSync(owner, "utf8");
	successful(state.invoke(["clean"]));
	assert.equal(readFileSync(owner, "utf8"), before);
	assert.equal(existsSync(output), true);
	successful(state.invoke(["clean", "--apply"]));
	assert.equal(existsSync(output), false);
	assert.equal(readFileSync(shared, "utf8"), "sentinel");
	assert.equal(readFileSync(music, "utf8"), "sentinel");
});

test("shared build lock prevents cleanup", async (context) => {
	const state = fixture(context);
	const ready = join(state.root, "ready");
	const child = spawn(
		"bash",
		[
			"scripts/run-with-storage.sh",
			"build",
			"bash",
			"-c",
			'printf ready > "$1"; read -r line',
			"fixture",
			ready,
		],
		{ cwd: state.checkout, env: state.env, stdio: ["pipe", "pipe", "pipe"] },
	);
	context.after(() => child.kill());
	try {
		for (let index = 0; !existsSync(ready) && index < 100; index++)
			await new Promise((resolve) => setTimeout(resolve, 20));
		assert.equal(existsSync(ready), true);
		put(join(state.resolved.CARGO_TARGET_DIR, "object"));
		assert.notEqual(state.invoke(["clean", "--apply"]).status, 0);
		assert.equal(
			existsSync(join(state.resolved.CARGO_TARGET_DIR, "object")),
			true,
		);
	} finally {
		child.stdin.end("finish\n");
		await new Promise((resolve) => child.once("close", resolve));
	}
});

test("prune removes orphan worktree storage and only expired active runs", (context) => {
	const state = fixture(context);
	successful(state.invoke(["lock-path"]));
	successful(state.invoke(["prepare"]));
	const linked = join(state.root, "linked");
	checked("git", ["-C", state.checkout, "worktree", "add", "--detach", linked]);
	const prepared = state.invoke(["lock-path"], {}, linked);
	successful(prepared);
	successful(state.invoke(["prepare"], {}, linked));
	const worktrees = join(state.resolved.EARTHLY_CLONE_ROOT, "worktrees");
	const orphan = readdirSync(worktrees).find(
		(name) => name !== state.resolved.EARTHLY_WORKTREE_ID,
	);
	checked("git", ["-C", state.checkout, "worktree", "remove", linked]);
	const runPaths = [];
	for (const age of [0, 8 * 24 * 60 * 60 * 1000]) {
		const run = state.invoke(["run"]);
		successful(run);
		const directory = run.stdout.trim().split("\n")[0];
		const marker = join(directory, "run.json");
		const data = JSON.parse(readFileSync(marker, "utf8"));
		context.after(() => rmSync(data.alias, { force: true }));
		data.createdAt -= age;
		writeFileSync(marker, JSON.stringify(data));
		runPaths.push(directory);
	}
	const shared = join(state.resolved.TURBO_CACHE_DIR, "preserve");
	put(shared);
	successful(state.invoke(["prune"]));
	assert.equal(existsSync(join(worktrees, orphan)), true);
	assert.equal(existsSync(runPaths[1]), true);
	successful(state.invoke(["prune", "--apply"]));
	assert.equal(existsSync(join(worktrees, orphan)), false);
	assert.equal(existsSync(runPaths[0]), true);
	assert.equal(existsSync(runPaths[1]), false);
	assert.equal(readFileSync(shared, "utf8"), "sentinel");
});

test("dry clean refuses unrecognized worktree storage without claiming ownership", (context) => {
	const state = fixture(context);
	mkdirSync(state.resolved.EARTHLY_WORKTREE_ROOT, { recursive: true });
	const result = state.invoke(["clean"]);
	assert.notEqual(
		result.status,
		0,
		"An unrecognized directory must not be adopted by a cleanup preview",
	);
	assert.deepEqual(readdirSync(state.resolved.EARTHLY_WORKTREE_ROOT), []);
});

test("migration backs up relocated Cargo metadata and retains compiled artifacts", (context) => {
	const state = fixture(context);
	const source = join(state.checkout, "desktop/src-tauri/target");
	for (const profile of [
		"debug",
		"release",
		"x86_64-unknown-linux-gnu/debug",
	]) {
		for (const name of [".fingerprint", "build", "deps", "incremental"])
			put(join(source, profile, name, "preserve"));
	}
	successful(state.invoke(["migrate"]));
	assert.equal(existsSync(state.env.EARTHLY_CACHE_ROOT), false);
	assert.equal(existsSync(join(source, "debug/build/preserve")), true);
	successful(state.invoke(["migrate", "--apply"]));
	const backupRoot = join(
		state.resolved.EARTHLY_WORKTREE_ROOT,
		"build/cargo-migration-backup",
	);
	const backup = join(backupRoot, readdirSync(backupRoot)[0]);
	for (const profile of [
		"debug",
		"release",
		"x86_64-unknown-linux-gnu/debug",
	]) {
		for (const name of [".fingerprint", "build"]) {
			assert.equal(
				existsSync(join(state.resolved.CARGO_TARGET_DIR, profile, name)),
				false,
			);
			assert.equal(
				readFileSync(join(backup, profile, name, "preserve"), "utf8"),
				"sentinel",
			);
		}
		for (const name of ["deps", "incremental"])
			assert.equal(
				existsSync(
					join(state.resolved.CARGO_TARGET_DIR, profile, name, "preserve"),
				),
				true,
			);
	}
});

test("refresh repairs an owned migrated target with read-only preview and rejects custom paths", (context) => {
	const state = fixture(context);
	successful(state.invoke(["lock-path"]));
	successful(state.invoke(["prepare"]));
	const stale = join(state.resolved.CARGO_TARGET_DIR, "debug/build/stale");
	put(stale);
	successful(state.invoke(["migrate", "--refresh-cargo"]));
	assert.equal(existsSync(stale), true);
	assert.equal(
		existsSync(
			join(
				state.resolved.EARTHLY_WORKTREE_ROOT,
				"build/cargo-migration-backup",
			),
		),
		false,
	);
	assert.notEqual(
		state.invoke(["migrate", "--refresh-cargo", "--apply"], {
			CARGO_TARGET_DIR: join(state.root, "custom"),
		}).status,
		0,
	);
	successful(state.invoke(["migrate", "--refresh-cargo", "--apply"]));
	assert.equal(existsSync(stale), false);
});

test("status reports inactive modes without requiring a local context", (context) => {
	const state = fixture(context);
	for (const mode of ["ci", "legacy", "clean-room"]) {
		const result = state.invoke(["status"], { EARTHLY_STORAGE_MODE: mode });
		successful(result);
		assert.match(result.stdout, new RegExp(`Mode: ${mode}`));
	}
	assert.equal(existsSync(state.env.EARTHLY_CACHE_ROOT), false);
});

test("refresh rejects unowned migrated targets without moving their metadata", (context) => {
	const state = fixture(context);
	const stale = join(state.resolved.CARGO_TARGET_DIR, "debug/build/stale");
	put(stale);
	assert.notEqual(state.invoke(["migrate", "--refresh-cargo"]).status, 0);
	assert.equal(readFileSync(stale, "utf8"), "sentinel");
	assert.equal(
		existsSync(join(state.resolved.EARTHLY_WORKTREE_ROOT, "owner.json")),
		false,
	);
});

test("migration previews and replaces only an empty destination directory", (context) => {
	const state = fixture(context);
	successful(state.invoke(["lock-path"]));
	successful(state.invoke(["prepare"]));
	const source = join(state.checkout, "desktop/src-tauri/target/object");
	put(source);
	mkdirSync(state.resolved.CARGO_TARGET_DIR, { recursive: true });
	const preview = state.invoke(["migrate"]);
	successful(preview);
	assert.match(preview.stdout, /Would move:/);
	assert.deepEqual(readdirSync(state.resolved.CARGO_TARGET_DIR), []);
	assert.equal(readFileSync(source, "utf8"), "sentinel");
	successful(state.invoke(["migrate", "--apply"]));
	assert.equal(existsSync(source), false);
	assert.equal(
		readFileSync(join(state.resolved.CARGO_TARGET_DIR, "object"), "utf8"),
		"sentinel",
	);
	put(source, "replacement");
	successful(state.invoke(["migrate", "--apply"]));
	assert.equal(readFileSync(source, "utf8"), "replacement");
	assert.equal(
		readFileSync(join(state.resolved.CARGO_TARGET_DIR, "object"), "utf8"),
		"sentinel",
	);
});
