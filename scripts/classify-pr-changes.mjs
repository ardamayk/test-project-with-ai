#!/usr/bin/env node

import { spawnSync } from "node:child_process";

const DEFAULT_DECISION = {
	workspace_fast: true,
	server_fast: true,
	desktop_unit: false,
	generated_drift: false,
	web_e2e: false,
	hls: false,
	real_mpv: false,
	integration_required: false,
};

const CONSERVATIVE_VALIDATION_OVERRIDES = {
	desktop_unit: true,
	generated_drift: true,
	web_e2e: true,
	hls: true,
	real_mpv: true,
};

const USAGE_MESSAGE =
	"Usage: classify-pr-changes.mjs --base <revision> --head <revision> [--format json|github]";
const ARGUMENT_NAMES = new Set(["--base", "--head", "--format"]);
const OUTPUT_FORMATS = new Set(["json", "github"]);

const GLOBAL_PATHS = new Set([
	"Dockerfile",
	"biome.json",
	"lint-staged.config.mjs",
	"mise.toml",
	"package.json",
	"pnpm-lock.yaml",
	"pnpm-workspace.yaml",
	"turbo.json",
]);

const CODE_GENERATION_PATHS = new Set([
	"scripts/check-openapi-generation.sh",
	"scripts/generate-openapi.sh",
]);

const HLS_WEB_PATHS = new Set([
	"web/e2e/fixtures/hls.html",
	"web/e2e/radio-hls-proxy.spec.ts",
	"web/playwright.hls.config.ts",
	"web/src/playback/BrowserPlaybackEngine.test.ts",
	"web/src/playback/BrowserPlaybackEngine.ts",
]);

function parseArguments(args) {
	const options = { format: "json" };
	for (let index = 0; index < args.length; index += 2) {
		const name = args[index];
		const value = args[index + 1];
		if (!ARGUMENT_NAMES.has(name) || value === undefined) {
			throw new Error(USAGE_MESSAGE);
		}
		options[name.slice(2)] = value;
	}
	if (!options.base || !options.head || !OUTPUT_FORMATS.has(options.format)) {
		throw new Error(USAGE_MESSAGE);
	}
	return options;
}

function parseChangedPaths(output) {
	const fields = output.split("\0");
	const paths = [];
	for (let index = 0; index < fields.length - 1; ) {
		const status = fields[index++];
		paths.push(fields[index++]);
		if (status.startsWith("R") || status.startsWith("C")) {
			paths.push(fields[index++]);
		}
	}
	return paths;
}

function readChangedPaths(baseRevision, headRevision) {
	const result = spawnSync(
		"git",
		[
			"diff",
			"--name-status",
			"-z",
			"--find-renames",
			`${baseRevision}...${headRevision}`,
		],
		{ encoding: "utf8" },
	);
	if (result.status !== 0) {
		throw new Error(result.stderr.trim() || "git diff failed");
	}
	return parseChangedPaths(result.stdout);
}

function isDocumentation(path) {
	return (
		path.startsWith("docs/") ||
		path.startsWith("packages/docs/content/") ||
		path.endsWith(".md") ||
		path.endsWith(".mdx") ||
		path === "LICENSE"
	);
}

function isContract(path) {
	return (
		path.startsWith("packages/contracts/") ||
		path.startsWith("packages/api-client/src/generated/") ||
		path.startsWith("server/internal/api/gen/") ||
		CODE_GENERATION_PATHS.has(path)
	);
}

function isGlobal(path) {
	return (
		GLOBAL_PATHS.has(path) ||
		path.startsWith(".github/") ||
		path.startsWith(".husky/") ||
		path.startsWith("scripts/")
	);
}

function applyPathPolicy(path, decision, reasons) {
	if (isDocumentation(path)) {
		reasons.add("documentation");
	} else if (isContract(path)) {
		Object.assign(decision, CONSERVATIVE_VALIDATION_OVERRIDES);
		reasons.add("contract");
	} else if (isGlobal(path)) {
		Object.assign(decision, CONSERVATIVE_VALIDATION_OVERRIDES);
		reasons.add("global");
	} else if (HLS_WEB_PATHS.has(path)) {
		decision.web_e2e = true;
		decision.hls = true;
		reasons.add("hls");
		reasons.add("web");
	} else if (
		path.startsWith("web/") ||
		path.startsWith("packages/ui/") ||
		path.startsWith("packages/api-client/")
	) {
		decision.web_e2e = true;
		reasons.add("web");
	} else if (path.startsWith("server/")) {
		decision.web_e2e = true;
		decision.hls = true;
		reasons.add("server");
	} else if (path.startsWith("desktop/")) {
		decision.desktop_unit = true;
		decision.real_mpv = true;
		reasons.add("desktop");
	} else {
		Object.assign(decision, CONSERVATIVE_VALIDATION_OVERRIDES);
		reasons.add("unknown");
	}
}

function classifyPaths(paths) {
	const decision = { ...DEFAULT_DECISION };
	const reasons = new Set();
	for (const path of paths) {
		applyPathPolicy(path, decision, reasons);
	}
	decision.integration_required =
		decision.web_e2e || decision.hls || decision.real_mpv;
	return { ...decision, reasons: [...reasons].sort() };
}

function formatDecision(decision, format) {
	if (format === "json") {
		return JSON.stringify(decision);
	}
	return Object.entries(decision)
		.map(
			([name, value]) =>
				`${name}=${Array.isArray(value) ? JSON.stringify(value) : String(value)}`,
		)
		.join("\n");
}

try {
	const options = parseArguments(process.argv.slice(2));
	const decision = classifyPaths(readChangedPaths(options.base, options.head));
	console.log(formatDecision(decision, options.format));
} catch (error) {
	console.error(error instanceof Error ? error.message : String(error));
	process.exitCode = 1;
}
