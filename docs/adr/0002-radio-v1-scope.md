# Radio v1 scope

Radio v1 includes user-specific Radio Station management, favorite and position fields for ordering, one `/radio` screen for local stations and Radio Browser discovery/import through the backend, backend-proxied playback, and backend Now Playing extraction from ICY metadata.

Radio Browser import supports both `stationUuid` refetch and submitted search-result fallback. Imported metadata such as homepage, favicon, tags, country, language, codec, and bitrate may be stored so the local database remains the source of truth after import.

Station detail pages and user-facing metadata editing are implemented as the first post-v1 radio slice. Users can open a saved station detail route and edit station name, stream URL, homepage, favicon, country, language, tags, codec, and bitrate. Normalized station identity remains limited to the stored local record and optional Radio Browser source/external ID.

Future work: drag reordering, visualizers, automatic station sync, scheduled health checks, LAN/private stream support after explicit allowlist design, geolocation-based discovery, advanced ranking, HLS ID3 metadata, and SSE/WebSocket metadata updates.
