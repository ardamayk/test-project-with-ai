# Capability-based client-server compatibility

The Music Server keeps `/api/v1` backward compatible and advertises Server Capabilities so separately released Playback Clients can disable unsupported behavior instead of requiring an exact version match. Clients must fail clearly when a required capability is absent; unknown optional capabilities are ignored.

## Considered Options

- Require identical client and server versions: rejected because independent release artifacts would be unnecessarily coupled.
- Allow incompatibilities to fail at runtime without negotiation: rejected because failures would appear late and without actionable explanation.
