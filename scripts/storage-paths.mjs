import { spawnSync } from "node:child_process";
import { createHash, randomUUID } from "node:crypto";
import {
	existsSync,
	linkSync,
	lstatSync,
	mkdirSync,
	readdirSync,
	readFileSync,
	rmSync,
	writeFileSync,
} from "node:fs";
import path from "node:path";

export const STORAGE_VERSION = 1;
export const RETENTION_MILLISECONDS = 7 * 24 * 60 * 60 * 1000;
export const SCCACHE_VERSION = "0.17.0";

export function run(command, args, options = {}) {
	const result = spawnSync(command, args, { stdio: "inherit", ...options });
	if (result.error) throw result.error;
	if (result.status !== 0)
		throw new Error(`${command} failed (${result.status ?? result.signal})`);
	return result.stdout?.toString().trim();
}

export function getContext() {
	const env = process.env;
	if (env.EARTHLY_STORAGE_RESOLVED_MODE !== "local")
		throw new Error("Storage management requires local mode.");
	for (const name of [
		"EARTHLY_CLONE_ROOT",
		"EARTHLY_WORKTREE_ROOT",
		"EARTHLY_CHECKOUT_ROOT",
		"EARTHLY_GIT_COMMON_DIR",
	]) {
		if (!env[name] || !path.isAbsolute(env[name]))
			throw new Error(`Missing absolute ${name}; run through Mise.`);
	}
	return {
		clone: env.EARTHLY_CLONE_ROOT,
		worktree: env.EARTHLY_WORKTREE_ROOT,
		checkout: env.EARTHLY_CHECKOUT_ROOT,
		common: env.EARTHLY_GIT_COMMON_DIR,
		worktreeId: env.EARTHLY_WORKTREE_ID,
	};
}

export function assertNoSymlinks(directory) {
	let current = path.resolve(directory);
	while (current !== path.dirname(current)) {
		if (lstatSync(current, { throwIfNoEntry: false })?.isSymbolicLink())
			throw new Error(`Refusing symbolic storage path: ${current}`);
		current = path.dirname(current);
	}
}

export function readOwner(directory) {
	assertNoSymlinks(directory);
	const marker = path.join(directory, "owner.json");
	if (!existsSync(marker) || lstatSync(marker).isSymbolicLink())
		throw new Error(`Unrecognized storage directory: ${directory}`);
	return JSON.parse(readFileSync(marker, "utf8"));
}

export function ensureOwner(directory, expected) {
	assertNoSymlinks(directory);
	mkdirSync(directory, { recursive: true });
	const marker = path.join(directory, "owner.json");
	if (!existsSync(marker)) {
		if (readdirSync(directory).some((name) => name !== "owner.json"))
			throw new Error(`Nonempty storage directory has no owner: ${directory}`);
		const temporary = `${directory}.owner-${randomUUID()}`;
		writeFileSync(temporary, JSON.stringify(expected), { flag: "wx" });
		try {
			linkSync(temporary, marker);
		} catch (error) {
			if (error.code !== "EEXIST") throw error;
		} finally {
			rmSync(temporary);
		}
	}
	const actual = readOwner(directory);
	if (JSON.stringify(actual) !== JSON.stringify(expected))
		throw new Error(`Storage ownership mismatch: ${directory}`);
}

export function prepareClone(context) {
	ensureOwner(context.clone, {
		version: STORAGE_VERSION,
		common: context.common,
	});
	const locks = path.join(context.clone, "locks");
	assertNoSymlinks(locks);
	mkdirSync(locks, { recursive: true });
	const lock = path.join(locks, `${context.worktreeId}.lock`);
	assertNoSymlinks(lock);
	return lock;
}

export function prepareWorktree(context) {
	ensureOwner(context.worktree, {
		version: STORAGE_VERSION,
		common: context.common,
		checkout: context.checkout,
	});
}

export function isManagedPath(context, candidate) {
	return (
		candidate &&
		path.resolve(candidate).startsWith(`${context.worktree}${path.sep}`)
	);
}

export function getSize(directory) {
	if (!existsSync(directory)) return 0;
	const info = lstatSync(directory);
	if (info.isSymbolicLink()) return 0;
	if (!info.isDirectory()) return info.blocks * 512;
	return readdirSync(directory).reduce(
		(total, name) => total + getSize(path.join(directory, name)),
		info.blocks * 512,
	);
}

export function getIdentifier(directory) {
	return createHash("sha256").update(path.resolve(directory)).digest("hex");
}

export function createRunId() {
	return `${Date.now()}-${randomUUID()}`;
}
