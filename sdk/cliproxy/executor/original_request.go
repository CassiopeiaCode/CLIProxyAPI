package executor

import (
	"bytes"
	"io"
	"os"
)

const defaultOriginalRequestSpoolThresholdBytes = 1 << 20

// OriginalRequestBody stores an inbound request body either in memory or in a temp file.
type OriginalRequestBody struct {
	size int
	data []byte
	path string
}

// NewOriginalRequestBody stores payload in memory below the threshold and spills to a temp file above it.
func NewOriginalRequestBody(payload []byte, threshold int) (*OriginalRequestBody, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	if threshold <= 0 {
		threshold = defaultOriginalRequestSpoolThresholdBytes
	}
	body := &OriginalRequestBody{size: len(payload)}
	if len(payload) <= threshold {
		body.data = bytes.Clone(payload)
		return body, nil
	}

	tmpFile, err := os.CreateTemp("", "cliproxy-original-request-*.json")
	if err != nil {
		return nil, err
	}
	tmpPath := tmpFile.Name()
	if _, err = tmpFile.Write(payload); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return nil, err
	}
	if err = tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}
	body.path = tmpPath
	return body, nil
}

func (b *OriginalRequestBody) InMemory() bool {
	return b != nil && b.path == ""
}

func (b *OriginalRequestBody) Len() int {
	if b == nil {
		return 0
	}
	return b.size
}

func (b *OriginalRequestBody) Open() (io.ReadCloser, error) {
	if b == nil {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	if b.path == "" {
		return io.NopCloser(bytes.NewReader(b.data)), nil
	}
	return os.Open(b.path)
}

func (b *OriginalRequestBody) Bytes() ([]byte, error) {
	if b == nil {
		return nil, nil
	}
	if b.path == "" {
		return bytes.Clone(b.data), nil
	}
	return os.ReadFile(b.path)
}

func (b *OriginalRequestBody) Cleanup() error {
	if b == nil || b.path == "" {
		return nil
	}
	err := os.Remove(b.path)
	b.path = ""
	return err
}

// OriginalRequestBytes returns the inbound request bytes from memory or the temp file.
func (o Options) OriginalRequestBytes() []byte {
	if len(o.OriginalRequest) > 0 {
		return o.OriginalRequest
	}
	if o.OriginalRequestBody == nil {
		return nil
	}
	payload, err := o.OriginalRequestBody.Bytes()
	if err != nil {
		return nil
	}
	return payload
}

// OriginalRequestOr returns the inbound request bytes when available, otherwise fallback.
func (o Options) OriginalRequestOr(fallback []byte) []byte {
	if payload := o.OriginalRequestBytes(); len(payload) > 0 {
		return payload
	}
	return fallback
}
