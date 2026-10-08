package httpapi

import (
	"testing"

	"github.com/brusapa/brinketask/internal/storage/storagetest"
)

// Most tests of this package need no database; the /me tests do. The
// container starts only if one of them runs.
func TestMain(m *testing.M) { storagetest.Main(m) }
