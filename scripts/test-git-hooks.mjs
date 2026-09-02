import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
	chmodSync,
	cpSync,
	mkdirSync,
	mkdtempSync,
	readFileSync,
	rmSync,
	symlinkSync,
	writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const REPOSITORY_ROOT = fileURLToPath(new URL("..", import.meta.url));

function run(repositoryPath, command, args, options = {}) {
	const result = spawnSync(command, args, {
		cwd: repositoryPath,
		encoding: "utf8",
		env: { ...process.env, NO_COLOR: "1", ...options.env },
	});

	if (options.expectedStatus === undefined) {
		assert.equal(
			result.status,
			0,
			`${command} ${args.join(" ")} failed:\n${result.stderr}${result.stdout}`,
		);
	} else {
		assert.equal(result.status, options.expectedStatus);
	}

	return result;
}

function writeExecutable(filePath, contents) {
	writeFileSync(filePath, contents);
	chmodSync(filePath, 0o755);
}

function createRepository() {
	const repositoryPath = mkdtempSync(path.join(tmpdir(), "git-hooks-test-"));
	const binPath = path.join(repositoryPath, ".test-bin");
	mkdirSync(binPath);
	mkdirSync(path.join(repositoryPath, "scripts"));
	cpSync(
		path.join(REPOSITORY_ROOT, ".husky"),
		path.join(repositoryPath, ".husky"),
		{
			recursive: true,
		},
	);
	cpSync(
		path.join(REPOSITORY_ROOT, "lint-staged.config.mjs"),
		path.join(repositoryPath, "lint-staged.config.mjs"),
	);
	cpSync(
		path.join(REPOSITORY_ROOT, "scripts", "run-graphify-hook.sh"),
		path.join(repositoryPath, "scripts", "run-graphify-hook.sh"),
	);
	symlinkSync(
		path.join(REPOSITORY_ROOT, "node_modules"),
		path.join(repositoryPath, "node_modules"),
	);
	writeFileSync(
		path.join(repositoryPath, "package.json"),
		'{"private":true,"type":"module"}\n',
	);

	const commandLogPath = path.join(repositoryPath, "commands.log");
	writeFileSync(commandLogPath, "");
	writeExecutable(
		path.join(binPath, "mise"),
		`#!/bin/sh\nprintf 'mise %s\\n' "$*" >> "${commandLogPath}"\n[ "\${FAIL_FAST_GATE:-0}" != "1" ]\n`,
	);
	writeExecutable(
		path.join(binPath, "graphify"),
		`#!/bin/sh\nprintf 'graphify %s\\n' "$*" >> "${commandLogPath}"\n`,
	);

	run(repositoryPath, "git", ["init", "--quiet", "--initial-branch=main"]);
	run(repositoryPath, "git", ["config", "user.email", "hooks@example.test"]);
	run(repositoryPath, "git", ["config", "user.name", "Hook Tests"]);
	run(repositoryPath, "git", ["config", "core.hooksPath", ".husky"]);
	writeFileSync(path.join(repositoryPath, "README.md"), "initial\n");
	run(repositoryPath, "git", ["add", "."]);
	run(repositoryPath, "git", [
		"commit",
		"--quiet",
		"--no-verify",
		"-m",
		"initial",
	]);

	return {
		binPath,
		commandLogPath,
		repositoryPath,
		remove() {
			rmSync(repositoryPath, { force: true, recursive: true });
		},
	};
}

function hookEnvironment(fixture, extra = {}) {
	return {
		PATH: `${fixture.binPath}:${process.env.PATH}`,
		...extra,
	};
}

test("pre-commit formats staged files while preserving unstaged intent", () => {
	const fixture = createRepository();
	try {
		const sourcePath = path.join(fixture.repositoryPath, "sample.ts");
		writeFileSync(sourcePath, "const first = 1;\nconst second = 2;\n");
		run(fixture.repositoryPath, "git", ["add", "sample.ts"]);
		run(fixture.repositoryPath, "git", [
			"commit",
			"--quiet",
			"--no-verify",
			"-m",
			"add source",
		]);

		writeFileSync(sourcePath, "const first={value:1};\nconst second = 2;\n");
		run(fixture.repositoryPath, "git", ["add", "sample.ts"]);
		writeFileSync(
			sourcePath,
			"const first={value:1};\nconst second={value:2};\n",
		);
		run(
			fixture.repositoryPath,
			"git",
			["commit", "--quiet", "-m", "format staged"],
			{
				env: hookEnvironment(fixture),
			},
		);

		const committed = run(fixture.repositoryPath, "git", [
			"show",
			"HEAD:sample.ts",
		]).stdout;
		assert.equal(committed, "const first = { value: 1 };\nconst second = 2;\n");
		assert.equal(
			readFileSync(sourcePath, "utf8"),
			"const first = { value: 1 };\nconst second={value:2};\n",
		);
	} finally {
		fixture.remove();
	}
});

test("pre-commit formats staged Go source", () => {
	const fixture = createRepository();
	try {
		writeFileSync(
			path.join(fixture.repositoryPath, "sample.go"),
			"package sample\n\nfunc Value( )int{return 1}\n",
		);
		run(fixture.repositoryPath, "git", ["add", "sample.go"]);
		run(
			fixture.repositoryPath,
			"git",
			["commit", "--quiet", "-m", "format go"],
			{
				env: hookEnvironment(fixture),
			},
		);
		assert.equal(
			run(fixture.repositoryPath, "git", ["show", "HEAD:sample.go"]).stdout,
			"package sample\n\nfunc Value() int { return 1 }\n",
		);
	} finally {
		fixture.remove();
	}
});

test("pre-commit rejects staged Rust source that cargo fmt would change", () => {
	const fixture = createRepository();
	try {
		const rustRoot = path.join(fixture.repositoryPath, "desktop", "src-tauri");
		mkdirSync(path.join(rustRoot, "src"), { recursive: true });
		writeFileSync(
			path.join(rustRoot, "Cargo.toml"),
			'[package]\nname = "hook-fixture"\nversion = "0.1.0"\nedition = "2021"\n',
		);
		writeFileSync(
			path.join(rustRoot, "src", "lib.rs"),
			"pub fn value()->i32{1}\n",
		);
		run(fixture.repositoryPath, "git", ["add", "desktop/src-tauri"]);
		run(
			fixture.repositoryPath,
			"git",
			["commit", "--quiet", "-m", "unformatted rust"],
			{
				env: hookEnvironment(fixture),
				expectedStatus: 1,
			},
		);
		assert.equal(
			run(
				fixture.repositoryPath,
				"git",
				["show", "HEAD:desktop/src-tauri/src/lib.rs"],
				{
					expectedStatus: 128,
				},
			).status,
			128,
		);
	} finally {
		fixture.remove();
	}
});

test("pre-commit verifies generated output only for generation inputs", () => {
	const fixture = createRepository();
	try {
		writeFileSync(path.join(fixture.repositoryPath, "notes.md"), "unrelated\n");
		run(fixture.repositoryPath, "git", ["add", "notes.md"]);
		run(
			fixture.repositoryPath,
			"git",
			["commit", "--quiet", "-m", "unrelated"],
			{
				env: hookEnvironment(fixture),
			},
		);
		assert.equal(readFileSync(fixture.commandLogPath, "utf8"), "");

		mkdirSync(path.join(fixture.repositoryPath, "packages", "contracts"), {
			recursive: true,
		});
		writeFileSync(
			path.join(
				fixture.repositoryPath,
				"packages",
				"contracts",
				"openapi.yaml",
			),
			"openapi: 3.1.0\n",
		);
		run(fixture.repositoryPath, "git", [
			"add",
			"packages/contracts/openapi.yaml",
		]);
		run(
			fixture.repositoryPath,
			"git",
			["commit", "--quiet", "-m", "contract"],
			{
				env: hookEnvironment(fixture),
			},
		);
		assert.equal(
			readFileSync(fixture.commandLogPath, "utf8"),
			"mise run generate:check\n",
		);
	} finally {
		fixture.remove();
	}
});

test("hooks remain bypassable", () => {
	const fixture = createRepository();
	try {
		const sourcePath = path.join(fixture.repositoryPath, "bypass.ts");
		writeFileSync(sourcePath, "const bypass={value:1};\n");
		run(fixture.repositoryPath, "git", ["add", "bypass.ts"]);
		run(fixture.repositoryPath, "git", [
			"commit",
			"--quiet",
			"--no-verify",
			"-m",
			"bypass",
		]);
		assert.equal(
			run(fixture.repositoryPath, "git", ["show", "HEAD:bypass.ts"]).stdout,
			"const bypass={value:1};\n",
		);
	} finally {
		fixture.remove();
	}
});

test("pre-push runs Fast Gate and propagates failures", () => {
	const fixture = createRepository();
	const remotePath = mkdtempSync(path.join(tmpdir(), "git-hooks-remote-"));
	try {
		run(remotePath, "git", ["init", "--quiet", "--bare"]);
		run(fixture.repositoryPath, "git", ["remote", "add", "origin", remotePath]);
		run(fixture.repositoryPath, "git", ["push", "origin", "main"], {
			env: hookEnvironment(fixture, { FAIL_FAST_GATE: "1" }),
			expectedStatus: 1,
		});
		assert.equal(
			readFileSync(fixture.commandLogPath, "utf8"),
			"mise run ci:fast\n",
		);

		run(
			fixture.repositoryPath,
			"git",
			["push", "--no-verify", "origin", "main"],
			{
				env: hookEnvironment(fixture),
			},
		);
	} finally {
		fixture.remove();
		rmSync(remotePath, { force: true, recursive: true });
	}
});

test("Graphify remains optional after commits and branch checkouts", () => {
	const fixture = createRepository();
	try {
		mkdirSync(path.join(fixture.repositoryPath, "graphify-out"));
		writeFileSync(path.join(fixture.repositoryPath, "tracked.txt"), "commit\n");
		run(fixture.repositoryPath, "git", ["add", "tracked.txt"]);
		run(
			fixture.repositoryPath,
			"git",
			["commit", "--quiet", "-m", "graph change"],
			{
				env: hookEnvironment(fixture),
			},
		);
		run(fixture.repositoryPath, "git", ["checkout", "--quiet", "-b", "other"], {
			env: hookEnvironment(fixture),
		});

		let commands = "";
		for (let attempt = 0; attempt < 20; attempt += 1) {
			commands = readFileSync(fixture.commandLogPath, "utf8");
			if (commands.match(/graphify update \./g)?.length === 2) {
				break;
			}
			Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 25);
		}
		assert.match(commands, /graphify update \./);
		assert.equal(commands.match(/graphify update \./g)?.length, 2);
	} finally {
		fixture.remove();
	}
});

test("Husky installation is disabled in CI and enabled for local development", () => {
	const repositoryPath = mkdtempSync(
		path.join(tmpdir(), "husky-install-test-"),
	);
	try {
		mkdirSync(path.join(repositoryPath, ".husky"));
		cpSync(
			path.join(REPOSITORY_ROOT, ".husky", "install.mjs"),
			path.join(repositoryPath, ".husky", "install.mjs"),
		);
		symlinkSync(
			path.join(REPOSITORY_ROOT, "node_modules"),
			path.join(repositoryPath, "node_modules"),
		);
		run(repositoryPath, "git", ["init", "--quiet"]);
		run(repositoryPath, "node", [".husky/install.mjs"], {
			env: { CI: "true" },
		});
		assert.equal(
			run(repositoryPath, "git", ["config", "--get", "core.hooksPath"], {
				expectedStatus: 1,
			}).stdout,
			"",
		);
		run(repositoryPath, "node", [".husky/install.mjs"], {
			env: { CI: "", HUSKY: "0" },
		});
		assert.equal(
			run(repositoryPath, "git", ["config", "--get", "core.hooksPath"], {
				expectedStatus: 1,
			}).stdout,
			"",
		);

		run(repositoryPath, "node", [".husky/install.mjs"], {
			env: { CI: "", HUSKY: "" },
		});
		assert.equal(
			run(repositoryPath, "git", [
				"config",
				"--get",
				"core.hooksPath",
			]).stdout.trim(),
			".husky/_",
		);
	} finally {
		rmSync(repositoryPath, { force: true, recursive: true });
	}
});
