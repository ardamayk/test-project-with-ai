package managedimport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

const UPLOAD_INACTIVITY_TIMEOUT = 30 * time.Second

type uploadDeadlineReader struct {
	source        io.Reader
	controller    *http.ResponseController
	isInterrupted atomic.Bool
}

func (reader *uploadDeadlineReader) Read(buffer []byte) (int, error) {
	if err := reader.controller.SetReadDeadline(time.Now().Add(UPLOAD_INACTIVITY_TIMEOUT)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return 0, err
	}
	if reader.isInterrupted.Load() {
		return 0, context.Canceled
	}
	count, err := reader.source.Read(buffer)
	if errors.Is(err, io.EOF) {
		if clearErr := reader.controller.SetReadDeadline(time.Time{}); clearErr != nil && !errors.Is(clearErr, http.ErrNotSupported) {
			return count, clearErr
		}
	}
	return count, err
}

func (reader *uploadDeadlineReader) Interrupt() error {
	reader.isInterrupted.Store(true)
	err := reader.controller.SetReadDeadline(time.Now())
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}
