package consolidation

import (
	"github.com/coollazy/UCollection/internal/store"
	"github.com/coollazy/UCollection/internal/tronclient"
)

// Deps are the dependencies this package's functions and HTTP handlers
// need.
type Deps struct {
	Pool                *store.Pool
	TronClient          *tronclient.Client
	USDTContractAddress string
}
