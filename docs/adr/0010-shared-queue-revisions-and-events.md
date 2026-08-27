# Shared queue revisions and events

The user Queue is shared across Playback Clients, carries a Queue Revision, and is synchronized through server-sent events while each Playback Session remains device-local. A currently playing Track continues if another client removes it or clears the Queue; when it ends, the Player follows the latest Queue or stops if none remains.

Mutations based on a stale Queue Revision are rejected. Clients may refetch and retry an unambiguous append or remove once, while replace and reorder conflicts require visible user resolution.
