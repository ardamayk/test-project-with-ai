export default {
	"*.{js,cjs,mjs,jsx,ts,tsx,json,jsonc}":
		"biome check --write --no-errors-on-unmatched",
	"*.go": "gofmt -w",
	"desktop/src-tauri/**/*.rs": () =>
		"cargo fmt --manifest-path desktop/src-tauri/Cargo.toml -- --check",
};
