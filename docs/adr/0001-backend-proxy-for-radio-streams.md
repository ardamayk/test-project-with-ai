# Backend proxy for radio streams

Radio Station playback flows through the backend instead of connecting the browser directly to external stream URLs. This lets the backend handle ICY metadata parsing, avoid browser CORS limitations, and expose Now Playing data consistently, at the cost of proxying radio bandwidth through the server.

The proxy initially allows only `http://` and `https://` streams and blocks localhost, private IP, and link-local targets for SSRF safety. LAN/private radio stream support is future work and needs an explicit allowlist design before it is enabled.
