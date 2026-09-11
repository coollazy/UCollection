package checkout

import (
	"github.com/coollazy/UCollection/internal/store"
)

// Deps are the dependencies this module's HTTP handlers need. The
// /checkout/{token} page is public (no login, 見技術架構設計第6節) and every
// URL it emits is relative, so unlike internal/api it does not need
// PublicOrigin — just the DB pool to look orders up by public_token.
type Deps struct {
	Pool *store.Pool
}
