package dispatcher

import (
	"testing"
	"time"

	"github.com/xtls/xray-core/common/buf"
)

type timeoutReaderStub struct {
	timeout time.Duration
}

func (r *timeoutReaderStub) ReadMultiBufferTimeout(timeout time.Duration) (buf.MultiBuffer, error) {
	r.timeout = timeout
	return nil, nil
}

func (r *timeoutReaderStub) ReadMultiBuffer() (buf.MultiBuffer, error) {
	return nil, nil
}

func TestCounterReaderPassesTimeoutThrough(t *testing.T) {
	reader := &timeoutReaderStub{}
	counter := &CounterReader{Reader: reader}

	if _, err := counter.ReadMultiBufferTimeout(25 * time.Millisecond); err != nil {
		t.Fatalf("ReadMultiBufferTimeout returned error: %v", err)
	}
	if got, want := reader.timeout, 25*time.Millisecond; got != want {
		t.Fatalf("unexpected timeout: got %s want %s", got, want)
	}
}
