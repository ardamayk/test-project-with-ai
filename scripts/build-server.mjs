import { cpSync, existsSync, mkdirSync, rmSync } from "node:fs";
import path from "node:path";
import { assertNoSymlinks, getContext, run } from "./storage-paths.mjs";

try {
	const root = process.cwd();
	if (process.env.EARTHLY_STORAGE_RESOLVED_MODE !== "local") {
		run("bash", ["scripts/sync-static.sh"]);
		run("go", ["build", "-o", "../bin/server", "./cmd/server"], {
			cwd: path.join(root, "server"),
		});
	} else {
		const context = getContext();
		const staging = path.join(context.worktree, "build/server-staging");
		assertNoSymlinks(staging);
		rmSync(staging, { recursive: true, force: true });
		const server = path.join(root, "server");
		cpSync(server, staging, {
			recursive: true,
			filter: (source) => {
				const relative = path.relative(server, source);
				return ![
					"data",
					"music",
					"internal/staticassets/web",
					"internal/staticassets/docs",
				].includes(relative);
			},
		});
		for (const [name, source] of [
			["web", process.env.EARTHLY_WEB_DIST],
			["docs", process.env.EARTHLY_DOCS_DIST],
		]) {
			if (!source || !existsSync(path.join(source, "index.html")))
				throw new Error(
					`Missing ${name} distribution; run mise run ${name}:build first.`,
				);
			cpSync(source, path.join(staging, "internal/staticassets", name), {
				recursive: true,
			});
		}
		mkdirSync(path.dirname(process.env.EARTHLY_SERVER_BINARY), {
			recursive: true,
		});
		run(
			"go",
			["build", "-o", process.env.EARTHLY_SERVER_BINARY, "./cmd/server"],
			{ cwd: staging, env: { ...process.env, GOWORK: "off" } },
		);
	}
} catch (error) {
	console.error(`Server build failed: ${error.message}`);
	process.exitCode = 1;
}
