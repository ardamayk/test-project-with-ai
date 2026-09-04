# Library module

Read APIs for Artists, Albums, Tracks, and Album Artwork, plus the
media-inspection seam shared with Managed Import.

The module never ingests, deletes, or replaces files. Managed Import owns
ingestion, Permanent Track Deletion, and Track Replacement (ADR 0015, ADR 0016);
the server never scans a server-side folder.
