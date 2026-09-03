//! Renderer trust boundary for the Desktop Client (issue #55).
//!
//! The Web UI runs inside the Tauri webview as untrusted content. These tests
//! pin the configuration that keeps it from reaching the filesystem or the
//! network directly: the renderer may only call the crate's own commands,
//! which accept opaque selection IDs rather than paths and send bytes to the
//! exact configured Music Server origin.

use serde_json::Value;

const CAPABILITIES: &str = include_str!("../capabilities/default.json");
const TAURI_CONFIG: &str = include_str!("../tauri.conf.json");
const CARGO_MANIFEST: &str = include_str!("../Cargo.toml");

/// Plugin permission prefixes that would let renderer content read files,
/// open native pickers, or make arbitrary requests without going through the
/// audited commands.
const FORBIDDEN_PERMISSION_PREFIXES: &[&str] = &[
    "dialog:",
    "fs:",
    "http:",
    "shell:",
    "opener:",
    "process:",
    "os:",
    "store:",
    "upload:",
    "websocket:",
];

/// Crates that would let native code parse authoritative metadata; ADR-level
/// intent is that only the Music Server inspects audio.
const METADATA_PARSER_CRATES: &[&str] = &[
    "lofty",
    "symphonia",
    "id3",
    "metaflac",
    "mp4ameta",
    "mp3-metadata",
    "audiotags",
    "claxon",
    "hound",
    "opus",
    "ogg",
];

#[test]
fn renderer_capability_grants_only_core_event_permissions() {
    let capabilities: Value = serde_json::from_str(CAPABILITIES).expect("capabilities JSON");
    let permissions = capabilities["permissions"]
        .as_array()
        .expect("permissions array")
        .iter()
        .map(|permission| {
            permission.as_str().map(str::to_owned).unwrap_or_else(|| {
                permission["identifier"]
                    .as_str()
                    .expect("identifier")
                    .to_owned()
            })
        })
        .collect::<Vec<_>>();

    assert_eq!(
        permissions,
        [
            "core:default",
            "core:event:allow-listen",
            "core:event:allow-unlisten"
        ]
    );
    for permission in &permissions {
        for prefix in FORBIDDEN_PERMISSION_PREFIXES {
            assert!(
                !permission.starts_with(prefix),
                "{permission} exposes {prefix} to renderer content"
            );
        }
    }
    assert_eq!(
        capabilities["windows"],
        serde_json::json!(["main"]),
        "capability must target only the main window"
    );
    assert!(
        capabilities.get("remote").is_none(),
        "capability must not extend to remote URLs"
    );
}

#[test]
fn desktop_window_never_grants_ipc_to_remote_content() {
    let config: Value = serde_json::from_str(TAURI_CONFIG).expect("tauri config JSON");
    let security = &config["app"]["security"];

    assert!(
        security.get("dangerousRemoteDomainIpcAccess").is_none(),
        "remote domains must not receive IPC access"
    );
    assert!(
        security
            .get("dangerousDisableAssetCspModification")
            .is_none(),
        "asset CSP hardening must stay enabled"
    );
    let csp = security["csp"].as_str().expect("CSP string");
    let connect_sources = csp
        .split(';')
        .map(str::trim)
        .find(|directive| directive.starts_with("connect-src"))
        .expect("connect-src directive")
        .split_whitespace()
        .skip(1)
        .collect::<Vec<_>>();
    assert_eq!(
        connect_sources,
        ["ipc:", "http://ipc.localhost", "http://127.0.0.1:43129"],
        "renderer fetches may only reach IPC and the private media proxy"
    );
    for directive in csp.split(';').map(str::trim) {
        for source in directive.split_whitespace().skip(1) {
            assert!(
                !matches!(source, "*" | "http:" | "https:" | "ws:" | "wss:")
                    && !source.contains('*'),
                "CSP directive allows arbitrary origins: {directive}"
            );
        }
    }
    let frontend = config["build"]["frontendDist"]
        .as_str()
        .expect("frontendDist");
    assert!(
        !frontend.starts_with("http"),
        "production window must load bundled assets, not a remote URL: {frontend}"
    );
    let windows = config["app"]["windows"].as_array().expect("windows");
    assert_eq!(windows.len(), 1);
    assert_eq!(windows[0]["label"], "main");
    assert!(
        windows[0].get("url").is_none(),
        "main window must not point at a remote URL"
    );
}

#[test]
fn native_code_has_no_audio_metadata_parser_dependency() {
    let manifest: toml::Value = toml::from_str(CARGO_MANIFEST).expect("Cargo manifest");
    let mut dependencies = Vec::new();
    for section in ["dependencies", "dev-dependencies", "build-dependencies"] {
        if let Some(table) = manifest.get(section).and_then(toml::Value::as_table) {
            dependencies.extend(table.keys().cloned());
        }
    }
    assert!(!dependencies.is_empty());
    for dependency in &dependencies {
        assert!(
            !METADATA_PARSER_CRATES.contains(&dependency.as_str()),
            "{dependency} would let native code parse authoritative metadata"
        );
    }
}
