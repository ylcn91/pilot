package architect

import (
	"errors"

	"github.com/ylcn91/pilot/internal/memory"
)

// errFixture is a shared sentinel error used by tests that need a collector
// failure without caring about the specific message.
var errFixture = errors.New("fixture failure")

// memoryQueryZero returns a zero-value MetricsQuery for tests that pass a
// churn collector but never reach its source.
func memoryQueryZero() memory.MetricsQuery {
	return memory.MetricsQuery{}
}
