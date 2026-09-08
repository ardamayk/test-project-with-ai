import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
	chmodSync,
	mkdtempSync,
	readFileSync,
	rmSync,
	writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const SCRIPT = fileURLToPath(
	new URL("prepare-native-ci-dependencies.sh", import.meta.url),
);

for (const exitCode of [0, 23]) {
	test(`native setup preserves exit code ${exitCode} across workflow steps`, () => {
		const directory = mkdtempSync(
			path.join(tmpdir(), "native-ci-dependencies-"),
		);
		const sudo = path.join(directory, "sudo");
		writeFileSync(
			sudo,
			`#!/bin/sh\necho controlled dependency installation\nsleep 0.1\nexit ${exitCode}\n`,
		);
		chmodSync(sudo, 0o700);
		const env = {
			...process.env,
			RUNNER_TEMP: directory,
			PATH: `${directory}:${process.env.PATH}`,
		};
		try {
			const started = spawnSync("bash", [SCRIPT, "start"], {
				env,
				encoding: "utf8",
				timeout: 5000,
			});
			assert.equal(started.status, 0, started.stderr);
			const finished = spawnSync("bash", [SCRIPT, "wait"], {
				env,
				encoding: "utf8",
				timeout: 5000,
			});
			assert.equal(finished.status, exitCode, finished.stderr);
			assert.match(finished.stdout, /controlled dependency installation/);
			assert.equal(
				readFileSync(
					path.join(directory, "native-ci-dependencies/status"),
					"utf8",
				).trim(),
				String(exitCode),
			);
		} finally {
			rmSync(directory, { recursive: true, force: true });
		}
	});
}
