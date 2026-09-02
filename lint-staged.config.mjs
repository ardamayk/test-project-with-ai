export default {
	"*.{js,cjs,mjs,jsx,ts,tsx,json,jsonc}":
		"biome check --write --no-errors-on-unmatched",
	"*.go": "gofmt -w",
	"desktop/src-tauri/**/*.rs": () => "bash scripts/check-staged-rust-format.sh",
};
