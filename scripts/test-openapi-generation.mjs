import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
	appendFileSync,
	cpSync,
	mkdirSync,
	mkdtempSync,
	readFileSync,
	rmSync,
	symlinkSync,
	writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import test from "node:test";
import { fileURLToPath } from "node:url";

const REPOSITORY_ROOT = fileURLToPath(new URL("..", import.meta.url));
const GENERATED_PATHS = [
	"packages/api-client/src/generated/schema.ts",
	"server/internal/api/gen/types.gen.go",
];

function runCommand(repositoryRoot, command, args) {
	return spawnSync(command, args, {
		cwd: repositoryRoot,
		encoding: "utf8",
		env: { ...process.env, NO_COLOR: "1" },
	});
}

function runGit(repositoryRoot, args) {
	const result = runCommand(repositoryRoot, "git", args);
	assert.equal(result.status, 0, result.stderr);
	return result.stdout;
}

function copyFixtureFiles(fixtureRoot) {
	for (const directory of [
		"packages/contracts",
		"packages/api-client/src/generated",
		"server/internal/api/gen",
		"scripts",
	]) {
		mkdirSync(`${fixtureRoot}/${directory}`, { recursive: true });
	}

	for (const path of [
		"packages/contracts/openapi.yaml",
		"packages/contracts/oapi-codegen.yaml",
		"packages/contracts/package.json",
		...GENERATED_PATHS,
		"scripts/generate-openapi.sh",
		"scripts/check-openapi-generation.sh",
	]) {
		cpSync(`${REPOSITORY_ROOT}/${path}`, `${fixtureRoot}/${path}`);
	}

	symlinkSync(
		`${REPOSITORY_ROOT}/packages/contracts/node_modules`,
		`${fixtureRoot}/packages/contracts/node_modules`,
		"dir",
	);
	writeFileSync(`${fixtureRoot}/.gitignore`, "node_modules\n");
}

function initializeFixtureRepository(fixtureRoot) {
	runGit(fixtureRoot, ["init", "--quiet"]);
	runGit(fixtureRoot, ["add", "."]);
	runGit(fixtureRoot, [
		"-c",
		"user.name=Test",
		"-c",
		"user.email=test@example.com",
		"commit",
		"--quiet",
		"-m",
		"fixture",
	]);
}

function createFixture() {
	const fixtureRoot = mkdtempSync(`${tmpdir()}/openapi-generation-`);
	copyFixtureFiles(fixtureRoot);
	initializeFixtureRepository(fixtureRoot);
	return fixtureRoot;
}

function checkGeneration(repositoryRoot) {
	return runCommand(repositoryRoot, "bash", [
		"scripts/check-openapi-generation.sh",
	]);
}

function generateOpenApi(repositoryRoot) {
	return runCommand(repositoryRoot, "bash", ["scripts/generate-openapi.sh"]);
}

function withFixture(callback) {
	const fixtureRoot = createFixture();
	try {
		callback(fixtureRoot);
	} finally {
		rmSync(fixtureRoot, { force: true, recursive: true });
	}
}

function snapshotRepository(repositoryRoot) {
	return {
		status: runGit(repositoryRoot, ["status", "--porcelain=v2"]),
		unstaged: runGit(repositoryRoot, ["diff", "--binary"]),
		staged: runGit(repositoryRoot, ["diff", "--cached", "--binary"]),
	};
}

function assertDrift(repositoryRoot, expectedPath) {
	const snapshotBefore = snapshotRepository(repositoryRoot);
	const result = checkGeneration(repositoryRoot);
	assert.notEqual(result.status, 0);
	assert.match(`${result.stderr}${result.stdout}`, new RegExp(expectedPath));
	assert.deepEqual(snapshotRepository(repositoryRoot), snapshotBefore);
}

test("matching generated OpenAPI outputs pass drift verification", () => {
	withFixture((fixtureRoot) => {
		const result = checkGeneration(fixtureRoot);
		assert.equal(result.status, 0, `${result.stderr}${result.stdout}`);
	});
});

test("explicit generation writes both committed OpenAPI outputs", () => {
	withFixture((fixtureRoot) => {
		const expectedOutputs = GENERATED_PATHS.map((path) =>
			readFileSync(`${fixtureRoot}/${path}`),
		);
		for (const path of GENERATED_PATHS) {
			writeFileSync(`${fixtureRoot}/${path}`, "stale\n");
		}

		const result = generateOpenApi(fixtureRoot);
		assert.equal(result.status, 0, `${result.stderr}${result.stdout}`);
		for (const [index, path] of GENERATED_PATHS.entries()) {
			assert.deepEqual(
				readFileSync(`${fixtureRoot}/${path}`),
				expectedOutputs[index],
			);
		}
	});
});

test("OpenAPI contract drift fails with actionable output", () => {
	withFixture((fixtureRoot) => {
		const contractPath = `${fixtureRoot}/packages/contracts/openapi.yaml`;
		const contract = readFileSync(contractPath, "utf8");
		writeFileSync(
			contractPath,
			contract.replace("version: 0.1.0", "version: 0.1.1"),
		);
		assertDrift(fixtureRoot, "server/internal/api/gen/types.gen.go");
	});
});

test("manually edited generated output fails with actionable output", () => {
	withFixture((fixtureRoot) => {
		const generatedPath = `${fixtureRoot}/${GENERATED_PATHS[0]}`;
		appendFileSync(generatedPath, "// stale generated output\n");
		assertDrift(fixtureRoot, "packages/api-client/src/generated/schema.ts");
	});
});

test("generator configuration drift fails with actionable output", () => {
	withFixture((fixtureRoot) => {
		const configPath = `${fixtureRoot}/packages/contracts/oapi-codegen.yaml`;
		const config = readFileSync(configPath, "utf8");
		writeFileSync(
			configPath,
			config.replace("package: gen", "package: generated"),
		);
		assertDrift(fixtureRoot, "server/internal/api/gen/types.gen.go");
	});
});

test("drift verification preserves dirty and partially staged changes", () => {
	withFixture((fixtureRoot) => {
		const unrelatedPath = `${fixtureRoot}/unrelated.txt`;
		writeFileSync(unrelatedPath, "staged change\n");
		runGit(fixtureRoot, ["add", "unrelated.txt"]);
		writeFileSync(unrelatedPath, "unstaged change\n");
		const snapshotBefore = snapshotRepository(fixtureRoot);

		const result = checkGeneration(fixtureRoot);
		assert.equal(result.status, 0, `${result.stderr}${result.stdout}`);
		assert.deepEqual(snapshotRepository(fixtureRoot), snapshotBefore);
	});
});
