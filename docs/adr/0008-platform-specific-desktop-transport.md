# Platform-specific desktop transport

The shared API client accepts an injected transport. The Web Client uses browser fetch, while the Desktop Client uses a Rust HTTP bridge restricted to the exact configured Music Server origin; packaged Tauri origins are not added broadly to server CORS. This keeps native capabilities behind local code and prevents remote or arbitrary web content from gaining general network access through the Desktop Client.
