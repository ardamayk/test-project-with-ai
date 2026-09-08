package playback

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const MAX_QUEUE_REQUEST_BYTES = 1024 * 1024

func decodeQueueRequest(w http.ResponseWriter, r *http.Request, body any) error {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MAX_QUEUE_REQUEST_BYTES))
	if err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			return fmt.Errorf("queue request exceeds %d bytes", MAX_QUEUE_REQUEST_BYTES)
		}
		return errors.New("could not read queue request body")
	}
	if err := json.Unmarshal(data, body); err != nil {
		return errors.New("invalid JSON body")
	}
	return nil
}
