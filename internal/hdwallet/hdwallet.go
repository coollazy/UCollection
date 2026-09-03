// Package hdwallet derives TRC20 receiving addresses from a master
// wallet's xpub. See docs/開發流程框架-03-技術架構設計.md 第3節 and
// docs/驗證結論-01-HD地址衍生.md.
//
// The server only ever handles xpub (a public key), never a mnemonic or
// private key — see CLAUDE.md 安全鐵律1.
package hdwallet

import (
	"fmt"

	"github.com/btcsuite/btcd/btcutil/base58"
	"github.com/btcsuite/btcd/btcutil/hdkeychain"
	"golang.org/x/crypto/sha3"
)

// tronAddressVersion is the single version byte Tron prepends before
// Base58Check-encoding an address (0x41), analogous to Bitcoin's 0x00.
const tronAddressVersion = 0x41

// DeriveAddress derives the Tron address for the non-hardened path
// m/0/index under xpub, an account-level extended public key already
// encoding the hardened path m/44'/195'/0' (技術架構設計第3節).
//
// index reservation (e.g. "index 0 is never assigned to an order") is an
// allocation-time business rule owned by whoever assigns indexes
// (internal/order, under the advisory lock) — this function is a stateless
// derivation and does not enforce it.
func DeriveAddress(xpub string, index uint32) (string, error) {
	acctKey, err := hdkeychain.NewKeyFromString(xpub)
	if err != nil {
		return "", fmt.Errorf("hdwallet: parse xpub: %w", err)
	}
	if acctKey.IsPrivate() {
		return "", fmt.Errorf("hdwallet: expected a public extended key (xpub), got a private extended key")
	}

	changeKey, err := acctKey.Derive(0)
	if err != nil {
		return "", fmt.Errorf("hdwallet: derive change key: %w", err)
	}

	childKey, err := changeKey.Derive(index)
	if err != nil {
		return "", fmt.Errorf("hdwallet: derive index %d: %w", index, err)
	}

	pubKey, err := childKey.ECPubKey()
	if err != nil {
		return "", fmt.Errorf("hdwallet: read public key: %w", err)
	}

	// Tron (like Ethereum) hashes the uncompressed public key with the
	// leading 0x04 prefix byte stripped, NOT the compressed form
	// hdkeychain returns by default. Using the compressed key here would
	// silently produce a mathematically well-formed but wrong address.
	uncompressed := pubKey.SerializeUncompressed()

	hash := sha3.NewLegacyKeccak256()
	hash.Write(uncompressed[1:])
	digest := hash.Sum(nil)

	addressHash := digest[len(digest)-20:]
	return base58.CheckEncode(addressHash, tronAddressVersion), nil
}
