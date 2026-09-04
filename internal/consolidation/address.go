package consolidation

import (
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/btcsuite/btcd/btcutil/base58"
)

// tronAddressVersion mirrors internal/hdwallet's constant of the same name
// (kept package-local rather than exported from hdwallet — this package
// only needs to validate/encode addresses, not derive them, and hdwallet
// is deliberately scoped to derivation only, see CLAUDE.md 安全鐵律1).
const tronAddressVersion = 0x41

// isValidTronAddress reports whether addr decodes as a well-formed Tron
// Base58Check address (version byte 0x41, 20-byte payload) — a Go
// equivalent of tronWeb.isAddress() (階段03技術細節暫存.md), used for address
// book entries and batch destination addresses.
func isValidTronAddress(addr string) bool {
	payload, version, err := base58.CheckDecode(addr)
	if err != nil {
		return false
	}
	return version == tronAddressVersion && len(payload) == 20
}

// abiEncodeTransfer ABI-encodes the parameter block for a TRC20
// transfer(address,uint256) call: destination left-padded to 32 bytes,
// amount left-padded to 32 bytes, concatenated (no function selector —
// TriggerSmartContract takes that separately, see prepare.go). This exact
// encoding was manually verified against a real triggersmartcontract call
// to Shasta during this module's development (see docs/進度.md).
func abiEncodeTransfer(destination string, amount int64) (string, error) {
	if amount <= 0 {
		return "", fmt.Errorf("consolidation: abiEncodeTransfer: amount must be positive, got %d", amount)
	}
	payload, version, err := base58.CheckDecode(destination)
	if err != nil {
		return "", fmt.Errorf("consolidation: abiEncodeTransfer: decode destination: %w", err)
	}
	if version != tronAddressVersion || len(payload) != 20 {
		return "", fmt.Errorf("consolidation: abiEncodeTransfer: destination is not a valid Tron address")
	}

	var addrParam [32]byte
	copy(addrParam[12:], payload)

	var amountParam [32]byte
	big.NewInt(amount).FillBytes(amountParam[:])

	return hex.EncodeToString(addrParam[:]) + hex.EncodeToString(amountParam[:]), nil
}
