# OpenAPI generation is explicit and its outputs are committed

Generated Go and TypeScript OpenAPI outputs remain committed, `generate` is the only task that writes them, and drift verification compares isolated output without changing the working tree. Build, check, unit-test, and integration tasks consume committed outputs instead of regenerating them implicitly. This keeps the contract authoritative while making ordinary tasks deterministic and preserving a reviewable record of generated API changes.
