# Managed Import owns strict, bit-preserving ingestion

Managed Import is the authoritative future ingestion path, and the Music Server owns uploaded copies from staging through Canonical Library Path placement in Managed Storage. Web and Desktop Clients submit files but do not establish metadata, identity, storage paths, or integrity. The Strict Import Profile requires explicit Track and Album identity metadata, ordered Artist and Album Artist credits, Track position, Genre, embedded front-cover Album Artwork, supported technical audio properties, and a successful full-stream decode; validation rejects a file with a structured error code instead of synthesizing fallback metadata.

Managed Storage is Bit-Preserving Storage: the Music Server retains the exact uploaded audio-file bytes without transcoding, tag rewriting, or artwork rewriting, and verifies the full-file SHA-256 before and after canonical placement. Extracted Album Artwork is a separate display copy and does not alter the authoritative audio file.

Legacy Tracks remain playable under the existing scanner during the transition, and introducing the media-inspection seam does not migrate scanner callers or change their fallback behavior. A later, explicit Library Migration must copy each accepted Legacy Track through the same Strict Import Profile and byte verification before any separately confirmed source cleanup.

The storage-byte guarantee ends at the authoritative file: it proves that Managed Storage retained the uploaded bytes. Playback Format Match separately reports whether a Playback Client decoded and submitted matching source parameters without known processing; it does not prove mathematically bit-identical DAC output, and Bit-Preserving Storage does not imply it.
