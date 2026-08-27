package playback

import "errors"

var ErrNotFound = errors.New("not found")
var ErrRevisionConflict = errors.New("queue revision conflict")
var ErrInvalidQueueOrder = errors.New("queue order does not match current items")
