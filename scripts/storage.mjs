import {
	existsSync,
	lstatSync,
	mkdirSync,
	readdirSync,
	readFileSync,
	readlinkSync,
	renameSync,
	rmSync,
	symlinkSync,
	writeFileSync,
} from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
	assertNoSymlinks,
	createRunId,
	getContext,
	getIdentifier,
	getSize,
	isManagedPath,
	prepareClone,
	prepareWorktree,
	RETENTION_MILLISECONDS,
	readOwner,
	run,
	SCCACHE_VERSION,
	STORAGE_VERSION,
} from "./storage-paths.mjs";

function printManagedUsage(context) {
	const worktrees = path.join(context.clone, "worktrees");
	if (existsSync(worktrees)) {
		assertNoSymlinks(worktrees);
		for (const entry of readdirSync(worktrees, { withFileTypes: true })) {
			if (!entry.isDirectory()) continue;
			const directory = path.join(worktrees, entry.name);
			const owner = validateWorktreeOwner(context, directory);
			console.log(
				`Worktree: ${owner.checkout} (${getSize(directory)} allocated bytes)`,
			);
		}
	}
	for (const relative of [
		"build/cargo-migration-backup",
		"build/cargo-target/debug/incremental",
		"build/cargo-target/release/incremental",
	]) {
		const directory = path.join(context.worktree, relative);
		if (existsSync(directory))
			console.log(
				`Retained migration/incremental data: ${directory} (${getSize(directory)} allocated bytes); reclaim after build validation`,
			);
	}
}

function printStatus(context) {
	console.log(
		`Mode: ${process.env.EARTHLY_STORAGE_RESOLVED_MODE}\nCheckout: ${context.checkout}`,
	);
	for (const name of [
		"CARGO_TARGET_DIR",
		"TURBO_CACHE_DIR",
		"EARTHLY_WEB_DIST",
		"EARTHLY_DOCS_DIST",
		"EARTHLY_SERVER_BINARY",
		"EARTHLY_RUN_ROOT",
	]) {
		console.log(
			`${name}: ${process.env[name]} (${getSize(process.env[name])} allocated bytes)`,
		);
	}
	printManagedUsage(context);
	console.log(
		`Compiler wrapper: ${process.env.RUSTC_WRAPPER || "inactive; run mise run cache:setup"}`,
	);
	if (process.env.RUSTC_WRAPPER?.includes("sccache"))
		run(process.env.EARTHLY_SCCACHE_BINARY || process.env.RUSTC_WRAPPER, [
			"--show-stats",
		]);
}

function startRun(context) {
	prepareWorktree(context);
	const root = process.env.EARTHLY_RUN_ROOT;
	assertNoSymlinks(root);
	const directory = path.join(root, createRunId());
	mkdirSync(path.join(directory, "tmp"), { recursive: true });
	// Unix sockets need a short pathname even when the actual cache root is long.
	const alias = path.join(
		"/tmp",
		`earthly-${process.getuid()}-${path.basename(directory).slice(-12)}`,
	);
	symlinkSync(path.join(directory, "tmp"), alias);
	writeFileSync(
		path.join(directory, "run.json"),
		JSON.stringify({
			version: STORAGE_VERSION,
			checkout: context.checkout,
			alias,
			createdAt: Date.now(),
		}),
	);
	console.log(directory);
	console.log(alias);
}

function removeAlias(directory) {
	const marker = path.join(directory, "run.json");
	if (!existsSync(marker)) return;
	assertNoSymlinks(marker);
	const { alias } = JSON.parse(readFileSync(marker, "utf8"));
	if (alias === undefined) return;
	if (
		typeof alias !== "string" ||
		!alias.startsWith(`/tmp/earthly-${process.getuid()}-`) ||
		path.dirname(alias) !== "/tmp"
	)
		throw new Error(`Invalid temporary alias in ${marker}`);
	if (
		existsSync(alias) &&
		lstatSync(alias).isSymbolicLink() &&
		readlinkSync(alias) === path.join(directory, "tmp")
	)
		rmSync(alias);
}

function cleanRuns(directory) {
	const runs = path.join(directory, "runs");
	if (!existsSync(runs)) return;
	assertNoSymlinks(runs);
	for (const entry of readdirSync(runs, { withFileTypes: true })) {
		if (entry.isDirectory()) removeAlias(path.join(runs, entry.name));
	}
}

function cleanCurrent(context, apply) {
	if (!existsSync(context.worktree)) return;
	const owner = readOwner(context.worktree);
	if (
		owner.common !== context.common ||
		owner.checkout !== context.checkout ||
		owner.version !== STORAGE_VERSION
	)
		throw new Error("Storage ownership mismatch");
	const candidates = ["build", "cache", "runs"].map((name) =>
		path.join(context.worktree, name),
	);
	for (const directory of candidates.filter(existsSync)) {
		console.log(`${apply ? "Remove" : "Would remove"}: ${directory}`);
		if (!apply) continue;
		assertNoSymlinks(directory);
		if (path.basename(directory) === "runs") cleanRuns(context.worktree);
		rmSync(directory, { recursive: true });
	}
}

function migrationPairs(context) {
	return [
		["desktop/src-tauri/target", process.env.CARGO_TARGET_DIR],
		["web/dist", process.env.EARTHLY_WEB_DIST],
		["packages/docs/dist", process.env.EARTHLY_DOCS_DIST],
		["bin/server", process.env.EARTHLY_SERVER_BINARY],
		["desktop/src-tauri/binaries", process.env.EARTHLY_MPV_DIRECTORY],
	].map(([source, destination]) => [
		path.join(context.checkout, source),
		destination,
	]);
}

function cargoMetadataDirectories(target) {
	const candidates = [];
	for (const entry of readdirSync(target, { withFileTypes: true })) {
		if (!entry.isDirectory()) continue;
		const directory = path.join(target, entry.name);
		for (const metadata of [".fingerprint", "build"]) {
			if (existsSync(path.join(directory, metadata)))
				candidates.push(path.join(directory, metadata));
		}
		// Cross-compiled artifacts add a target-triple directory above the profile.
		for (const profile of readdirSync(directory, { withFileTypes: true })) {
			if (
				!profile.isDirectory() ||
				["deps", "build", ".fingerprint", "incremental"].includes(profile.name)
			)
				continue;
			for (const metadata of [".fingerprint", "build"]) {
				const candidate = path.join(directory, profile.name, metadata);
				if (existsSync(candidate)) candidates.push(candidate);
			}
		}
	}
	return candidates;
}

function backupCargoMetadata(context, target, apply) {
	assertNoSymlinks(target);
	const backup = path.join(
		context.worktree,
		"build/cargo-migration-backup",
		createRunId(),
	);
	for (const source of cargoMetadataDirectories(target)) {
		assertNoSymlinks(source);
		const destination = path.join(backup, path.relative(target, source));
		console.log(
			`${apply ? "Back up" : "Would back up"} relocated Cargo metadata: ${source} -> ${destination}`,
		);
		if (!apply) continue;
		assertNoSymlinks(destination);
		mkdirSync(path.dirname(destination), { recursive: true });
		renameSync(source, destination);
	}
}

function refreshCargo(context, apply) {
	const target = process.env.CARGO_TARGET_DIR;
	if (target !== path.join(context.worktree, "build/cargo-target"))
		throw new Error("Refusing to refresh custom Cargo target");
	if (!existsSync(target)) return;
	const owner = readOwner(context.worktree);
	if (
		owner.version !== STORAGE_VERSION ||
		owner.common !== context.common ||
		owner.checkout !== context.checkout
	)
		throw new Error("Storage ownership mismatch");
	backupCargoMetadata(context, target, apply);
}

function migrate(context, apply) {
	for (const [source, destination] of migrationPairs(context)) {
		if (!existsSync(source)) continue;
		if (!isManagedPath(context, destination)) {
			console.log(`Skip custom destination: ${destination}`);
			continue;
		}
		assertNoSymlinks(source);
		assertNoSymlinks(destination);
		const tracked = run(
			"git",
			["-C", context.checkout, "ls-files", "--", source],
			{ stdio: "pipe" },
		);
		if (tracked)
			throw new Error(`Refusing to migrate tracked files: ${source}`);
		if (existsSync(destination)) {
			console.log(`Skip occupied destination: ${destination}`);
			continue;
		}
		console.log(
			`${apply ? "Move" : "Would move"}: ${source} -> ${destination}`,
		);
		if (apply) {
			prepareWorktree(context);
			mkdirSync(path.dirname(destination), { recursive: true });
			renameSync(source, destination);
		}
		if (source === path.join(context.checkout, "desktop/src-tauri/target"))
			backupCargoMetadata(context, apply ? destination : source, apply);
	}
	console.log(
		"Incremental artifacts are retained; validate a build before cleaning them.",
	);
}

function validateWorktreeOwner(context, directory) {
	const owner = readOwner(directory);
	if (
		owner.version !== STORAGE_VERSION ||
		owner.common !== context.common ||
		typeof owner.checkout !== "string" ||
		!path.isAbsolute(owner.checkout) ||
		getIdentifier(owner.checkout) !== path.basename(directory)
	)
		throw new Error(`Foreign storage: ${directory}`);
	return owner;
}

function prune(context, apply) {
	const root = path.join(context.clone, "worktrees");
	if (!existsSync(root)) return;
	assertNoSymlinks(root);
	const active = run(
		"git",
		["-C", context.checkout, "worktree", "list", "--porcelain", "-z"],
		{ stdio: "pipe" },
	);
	const checkouts = new Set(
		active
			.split("\0")
			.filter((entry) => entry.startsWith("worktree "))
			.map((entry) => entry.slice(9)),
	);
	for (const entry of readdirSync(root, { withFileTypes: true })) {
		if (!entry.isDirectory())
			throw new Error(`Unrecognized worktree storage: ${entry.name}`);
		const directory = path.join(root, entry.name);
		const owner = validateWorktreeOwner(context, directory);
		pruneWorktree(
			context,
			directory,
			owner,
			checkouts.has(owner.checkout),
			apply,
		);
	}
}

function pruneWorktree(context, directory, owner, isActive, apply) {
	const candidates = [];
	if (!isActive) candidates.push(directory);
	else if (existsSync(path.join(directory, "runs"))) {
		for (const entry of readdirSync(path.join(directory, "runs"), {
			withFileTypes: true,
		})) {
			if (!entry.isDirectory()) continue;
			const runDirectory = path.join(directory, "runs", entry.name);
			const marker = path.join(runDirectory, "run.json");
			if (!existsSync(marker))
				throw new Error(`Unrecognized run: ${runDirectory}`);
			assertNoSymlinks(marker);
			const data = JSON.parse(readFileSync(marker, "utf8"));
			if (
				data.version !== STORAGE_VERSION ||
				!Number.isFinite(data.createdAt) ||
				data.checkout !== owner.checkout
			)
				throw new Error(`Foreign run: ${runDirectory}`);
			if (Date.now() - data.createdAt > RETENTION_MILLISECONDS)
				candidates.push(runDirectory);
		}
	}
	for (const candidate of candidates) {
		console.log(`${apply ? "Prune" : "Would prune"}: ${candidate}`);
		if (apply) runLockedPrune(context, directory, candidate);
	}
}

function runLockedPrune(context, directory, candidate) {
	const lock = path.join(
		context.clone,
		"locks",
		`${path.basename(directory)}.lock`,
	);
	assertNoSymlinks(lock);
	run("flock", [
		"--exclusive",
		"--nonblock",
		lock,
		process.execPath,
		fileURLToPath(import.meta.url),
		"remove-owned",
		directory,
		candidate,
	]);
}

function removeOwned(context, directory, candidate) {
	const parent = path.join(context.clone, "worktrees");
	if (path.dirname(directory) !== parent)
		throw new Error("Invalid worktree removal root");
	validateWorktreeOwner(context, directory);
	if (
		candidate !== directory &&
		path.dirname(candidate) !== path.join(directory, "runs")
	)
		throw new Error("Invalid removal path");
	assertNoSymlinks(candidate);
	if (candidate === directory) cleanRuns(directory);
	else removeAlias(candidate);
	rmSync(candidate, { recursive: true });
}

function setupCompiler() {
	run("mise", ["install", `sccache@${SCCACHE_VERSION}`]);
	const binary = run(
		"mise",
		[
			"exec",
			`sccache@${SCCACHE_VERSION}`,
			"--",
			"sh",
			"-c",
			"command -v sccache",
		],
		{ stdio: "pipe" },
	);
	if (!binary || !path.isAbsolute(binary) || !existsSync(binary))
		throw new Error("Mise did not resolve the pinned sccache executable");
	const tools = path.join(
		process.env.XDG_CACHE_HOME || path.join(process.env.HOME, ".cache"),
		"earthly-audio/tools",
	);
	assertNoSymlinks(tools);
	mkdirSync(tools, { recursive: true });
	writeFileSync(path.join(tools, "sccache.path"), `${binary}\n`);
	console.log(
		`Installed sccache ${SCCACHE_VERSION}; local compilation will use it on the next Mise invocation.`,
	);
}

function main() {
	const [action, ...args] = process.argv.slice(2);
	if (
		action === "status" &&
		process.env.EARTHLY_STORAGE_RESOLVED_MODE !== "local"
	) {
		console.log(
			`Mode: ${process.env.EARTHLY_STORAGE_RESOLVED_MODE || process.env.EARTHLY_STORAGE_MODE || "auto"}\nLocal storage routing is inactive.`,
		);
		return;
	}
	const context = getContext();
	const apply = args.includes("--apply");
	if (
		["clean", "migrate"].includes(action) &&
		apply &&
		!args.includes("--locked")
	) {
		const lock = prepareClone(context);
		return run("flock", [
			"--exclusive",
			"--nonblock",
			lock,
			process.execPath,
			fileURLToPath(import.meta.url),
			action,
			...args,
			"--locked",
		]);
	}
	switch (action) {
		case "lock-path":
			console.log(prepareClone(context));
			break;
		case "prepare":
			prepareWorktree(context);
			break;
		case "run":
			startRun(context);
			break;
		case "finish-run":
			removeAlias(args[0]);
			break;
		case "status":
			printStatus(context);
			break;
		case "setup":
			setupCompiler();
			break;
		case "migrate":
			if (args.includes("--refresh-cargo")) refreshCargo(context, apply);
			else migrate(context, apply);
			break;
		case "clean":
			cleanCurrent(context, apply);
			break;
		case "prune":
			prune(context, apply);
			break;
		case "remove-owned":
			removeOwned(context, args[0], args[1]);
			break;
		default:
			throw new Error(`Unknown storage command: ${action}`);
	}
}

try {
	main();
} catch (error) {
	console.error(`Storage operation failed: ${error.message}`);
	process.exitCode = 1;
}
