-- +goose Up
CREATE TABLE IF NOT EXISTS user_preferences (
    user_id TEXT PRIMARY KEY,
    theme TEXT NOT NULL DEFAULT 'system',
    layout_json TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO user_preferences (user_id, theme, layout_json)
VALUES (
    '00000000-0000-0000-0000-000000000001',
    'system',
    '{"sidebarPosition":"left","panels":{"left":["now-playing","queue"],"right":["discover"]},"collapsed":{"left":false,"right":true}}'
);

-- +goose Down
DROP TABLE IF EXISTS user_preferences;
