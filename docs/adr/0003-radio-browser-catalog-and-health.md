# Radio Browser catalog and station health

The add-radio flow opens a Radio Browser catalog page instead of importing directly from the saved-stations screen. Catalog entries are sorted A-Z, can be searched and filtered, can be previewed without import, and only become local Radio Stations after an explicit import.

Country and tag/genre filters are proxied from Radio Browser option endpoints instead of being hardcoded in the client. Country options include the Radio Browser ISO country code when available so the UI can render a flag. The format filter treats AAC as an AAC family filter, covering both AAC and AAC+ catalog entries.

Catalog entries use direct manipulation: left click previews without import, right click opens a context menu for import or details. Details show the full tag set and catalog metadata while compact cards keep only the most useful scan data visible.

Radio Browser health metadata is shown for catalog entries, but custom/manual Radio Stations use Local Station Health measured by this app. Local health checks run on saved stations at a slower background cadence, with an extra check when a station is opened or played, so the app avoids probing the whole external catalog while still giving useful feedback for user-owned streams.

Radio Browser health remains advisory. A station marked healthy can still fail to play because the remote stream changed, requires different headers, is geo-blocked, uses a browser-unsupported container/codec, or fails during this app's stream proxy request. Preview failures are surfaced in the discover UI rather than treated as import failures.
