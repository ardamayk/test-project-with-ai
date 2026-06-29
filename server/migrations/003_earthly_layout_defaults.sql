-- +goose Up
UPDATE user_preferences
SET layout_json = '{"sidebarPosition":"left","panels":{"left":["now-playing"],"right":["discover"]},"collapsed":{"left":false,"right":false},"sizes":[22,50,28]}',
    theme = '{"mode":"system","preset":"earthly"}'
WHERE user_id = '00000000-0000-0000-0000-000000000001';

-- +goose Down
UPDATE user_preferences
SET layout_json = '{"sidebarPosition":"left","panels":{"left":["now-playing","queue"],"right":["discover"]},"collapsed":{"left":false,"right":true}}',
    theme = 'system'
WHERE user_id = '00000000-0000-0000-0000-000000000001';
