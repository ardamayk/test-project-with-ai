import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { cpSync, existsSync, mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

function run(command, args, options = {}) {
	const result = spawnSync(command, args, { encoding: "utf8", ...options });
	assert.equal(result.status, 0, result.stderr);
	return result.stdout;
}

function fixture(context) {
	const root = mkdtempSync(path.join(tmpdir(), "storage paths "));
	context.after(() => rmSync(root, { recursive: true, force: true }));
	const checkout = path.join(root, "checkout with spaces");
	mkdirSync(path.join(checkout, "scripts"), { recursive: true });
	cpSync(
		new URL("./storage-env.sh", import.meta.url),
		path.join(checkout, "scripts/storage-env.sh"),
	);
	run("git", ["init", "-q", checkout]);
	run("git", ["-C", checkout, "add", "."]);
	run("git", [
		"-C",
		checkout,
		"-c",
		"user.name=Test",
		"-c",
		"user.email=test@example.invalid",
		"-c",
		"core.hooksPath=/dev/null",
		"commit",
		"-qm",
		"fixture",
	]);
	return { root, checkout, cache: path.join(root, "external cache") };
}

function resolve(fixture, extra = {}, suffix = "") {
	const env = Object.fromEntries(
		Object.entries(process.env).filter(
			([name]) =>
				!name.startsWith("EARTHLY_") &&
				!["CI", "GITHUB_ACTIONS", "CARGO_TARGET_DIR", "RUSTC_WRAPPER"].includes(
					name,
				),
		),
	);
	const output = run(
		"bash",
		["-c", `source scripts/storage-env.sh || exit; ${suffix} env -0`],
		{
			cwd: fixture.checkout,
			env: { ...env, EARTHLY_CACHE_ROOT: fixture.cache, ...extra },
		},
	);
	return Object.fromEntries(
		output
			.split("\0")
			.filter(Boolean)
			.map((entry) => {
				const index = entry.indexOf("=");
				return [entry.slice(0, index), entry.slice(index + 1)];
			}),
	);
}

test("local resolution is read-only and stable across branch switches", (context) => {
	const current = fixture(context);
	const before = resolve(current);
	run("git", ["-C", current.checkout, "switch", "-qc", "another"]);
	const after = resolve(current);
	assert.equal(before.CARGO_TARGET_DIR, after.CARGO_TARGET_DIR);
	assert.equal(after.EARTHLY_STORAGE_RESOLVED_MODE, "local");
	assert.ok(after.CARGO_TARGET_DIR.startsWith(current.cache));
	assert.equal(existsSync(current.cache), false);
});

test("linked worktrees share a clone cache but isolate target directories", (context) => {
	const current = fixture(context);
	const linked = path.join(current.root, "linked checkout");
	run("git", [
		"-C",
		current.checkout,
		"worktree",
		"add",
		"-qb",
		"linked",
		linked,
	]);
	const first = resolve(current);
	const second = resolve({ ...current, checkout: linked });
	assert.equal(first.TURBO_CACHE_DIR, second.TURBO_CACHE_DIR);
	assert.notEqual(first.CARGO_TARGET_DIR, second.CARGO_TARGET_DIR);
});

test("CI, clean-room and legacy do not inject local defaults", (context) => {
	const current = fixture(context);
	for (const extra of [
		{ CI: "true" },
		{ GITHUB_ACTIONS: "true" },
		{ EARTHLY_STORAGE_MODE: "clean-room" },
		{ EARTHLY_STORAGE_MODE: "legacy" },
	]) {
		const result = resolve(current, extra);
		assert.equal(result.CARGO_TARGET_DIR, undefined);
		assert.equal(result.EARTHLY_WORKTREE_ROOT, undefined);
	}
});

test("explicit overrides survive repeated resolution and managed defaults do not leak", (context) => {
	const current = fixture(context);
	const custom = resolve(
		current,
		{ CARGO_TARGET_DIR: "/custom/target" },
		"source scripts/storage-env.sh;",
	);
	assert.equal(custom.CARGO_TARGET_DIR, "/custom/target");
	const isolated = resolve(
		current,
		{},
		"export EARTHLY_STORAGE_MODE=clean-room; source scripts/storage-env.sh;",
	);
	assert.equal(isolated.CARGO_TARGET_DIR, undefined);
	assert.equal(isolated.EARTHLY_WEB_DIST, undefined);
});
