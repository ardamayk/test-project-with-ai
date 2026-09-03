import { describe, expect, it } from 'vitest';
import {
  API_V1_CAPABILITY,
  hasServerCapability,
  LIBRARY_MIGRATION_CAPABILITY,
  MANAGED_IMPORT_BATCHES_CAPABILITY,
  MANAGED_IMPORT_CAPABILITY,
  MANAGED_TRACK_DELETION_CAPABILITY,
  MANAGED_TRACK_REPLACEMENT_CAPABILITY,
  missingServerCapabilities,
} from './capabilities';

const currentServer = {
  status: 'ok' as const,
  version: '0.1.0',
  capabilities: [
    API_V1_CAPABILITY,
    'playback.queue-events.v1',
    MANAGED_IMPORT_CAPABILITY,
    MANAGED_IMPORT_BATCHES_CAPABILITY,
    MANAGED_TRACK_DELETION_CAPABILITY,
    MANAGED_TRACK_REPLACEMENT_CAPABILITY,
    LIBRARY_MIGRATION_CAPABILITY,
  ],
};

describe('Server Capability gating', () => {
  it('accepts either a health response or a bare capability list', () => {
    expect(hasServerCapability(currentServer, MANAGED_IMPORT_CAPABILITY)).toBe(
      true,
    );
    expect(
      hasServerCapability(
        currentServer.capabilities,
        LIBRARY_MIGRATION_CAPABILITY,
      ),
    ).toBe(true);
  });

  it('gates on the exact capability name only', () => {
    const olderServer = [
      API_V1_CAPABILITY,
      'managed-import.v2',
      'managed-import',
    ];
    expect(hasServerCapability(olderServer, MANAGED_IMPORT_CAPABILITY)).toBe(
      false,
    );
    expect(hasServerCapability(olderServer, 'managed-import.v2')).toBe(true);
  });

  it('ignores unknown optional capabilities from a newer Music Server', () => {
    const newerServer = [
      ...currentServer.capabilities,
      'future.optional-feature.v9',
      'playback.party-mode.v1',
    ];
    expect(hasServerCapability(newerServer, MANAGED_IMPORT_CAPABILITY)).toBe(
      true,
    );
    expect(hasServerCapability(newerServer, 'unknown.capability')).toBe(false);
    expect(
      missingServerCapabilities(newerServer, [
        API_V1_CAPABILITY,
        MANAGED_IMPORT_BATCHES_CAPABILITY,
        MANAGED_TRACK_DELETION_CAPABILITY,
      ]),
    ).toEqual([]);
  });

  it('names every required capability an older Music Server lacks', () => {
    const olderServer = [API_V1_CAPABILITY, MANAGED_IMPORT_CAPABILITY];
    expect(
      missingServerCapabilities(olderServer, [
        API_V1_CAPABILITY,
        MANAGED_IMPORT_BATCHES_CAPABILITY,
        LIBRARY_MIGRATION_CAPABILITY,
      ]),
    ).toEqual([
      MANAGED_IMPORT_BATCHES_CAPABILITY,
      LIBRARY_MIGRATION_CAPABILITY,
    ]);
  });

  it('treats a missing health response as advertising nothing', () => {
    expect(hasServerCapability(undefined, API_V1_CAPABILITY)).toBe(false);
    expect(hasServerCapability(null, API_V1_CAPABILITY)).toBe(false);
    expect(missingServerCapabilities(undefined, [API_V1_CAPABILITY])).toEqual([
      API_V1_CAPABILITY,
    ]);
  });
});
