# Earthly Audio Desktop

Linux Tauri development client. It builds the shared React source from `web/` and embeds those assets; it never loads UI assets from a Music Server.

Start the separate Music Server on loopback, then launch the Desktop Client:

```bash
cd server && go run ./cmd/server
pnpm --dir desktop desktop:dev
```

The connection screen accepts `http://localhost`, `http://127.0.0.1`, or IPv6 loopback origins. Desktop JSON requests use bounded Tauri commands, and covers use a bounded cover-only custom protocol. Track, radio, and HLS audio use a Rust-owned streaming proxy bound to the dedicated `127.0.0.1:43129` origin allowed by the Desktop Content Security Policy. Its URL contains a random 256-bit capability token, serves only streaming API paths, and forwards only to the saved exact Music Server origin. It preserves Range responses, rewrites signed HLS child paths through the tokenized proxy, and streams audio without buffering it in renderer IPC.

The proxy starts with the Desktop Client, rejects redirects and non-media methods, and shuts down with Tauri application state. A second Desktop Client instance or another process occupying port `43129` makes startup fail closed with an actionable error. Bounded command responses retain the 32 MiB safety limit; only HLS manifests are buffered, with a separate 1 MiB limit, so their child URIs can be rewritten safely.

The Desktop Client does not bundle a Go server, database, or server lifecycle.
