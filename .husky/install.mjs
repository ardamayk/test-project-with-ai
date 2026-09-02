if (
	process.env.CI ||
	process.env.NODE_ENV === "production" ||
	process.env.HUSKY === "0"
) {
	process.exit(0);
}

const husky = (await import("husky")).default;
const result = husky();

if (result) {
	console.error(result);
	process.exit(1);
}
