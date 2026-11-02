package cursor

import (
	"errors"
	"strings"
	"testing"
)

var errPipeBroken = errors.New("pipe broke")

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) {
	return 0, errPipeBroken
}

func TestDrainPipeReturnsReadError(t *testing.T) {
	if err := drainPipe(brokenReader{}); !errors.Is(err, errPipeBroken) {
		t.Fatalf("drainPipe error = %v, want %v", err, errPipeBroken)
	}
}

func TestDrainPipeConsumesEverything(t *testing.T) {
	if err := drainPipe(strings.NewReader("left over output")); err != nil {
		t.Fatalf("drainPipe error = %v, want nil", err)
	}
}
