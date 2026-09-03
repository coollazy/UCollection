package hdwallet

import (
	"crypto/rand"
	"testing"

	"github.com/btcsuite/btcd/btcutil/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg"
)

// testXpub is the account-level xpub (m/44'/195'/0') derived from the
// standard BIP39 test mnemonic ("abandon"x11 + "about"). It is cross-checked
// against four independent implementations (two Node.js library
// combinations, one Swift, one Go) in
// docs/驗證結論-01-HD地址衍生.md and docs/評估-Go打包Docker可行性.md — the
// expected addresses below come from that cross-check, not from this
// package's own output.
const testXpub = "xpub6D1AabNHCupeiLM65ZR9UStMhJ1vCpyV4XbZdyhMZBiJXALQtmn9p42VTQckoHVn8WNqS7dqnJokZHAHcHGoaQgmv8D45oNUKx6DZMNZBCd"

func TestDeriveAddress_KnownVectors(t *testing.T) {
	want := map[uint32]string{
		0: "TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH",
		1: "TSeJkUh4Qv67VNFwY8LaAxERygNdy6NQZK",
		2: "TYJPRrdB5APNeRs4R7fYZSwW3TcrTKw2gx",
		3: "TRhVWK5XEDkQBDevcdCWW7RW51aRncty4W",
		4: "TT2X2yyubp7qpAWYYNE5JQWBtoZ7ikQFsY",
	}

	seen := make(map[string]bool)
	for index, wantAddr := range want {
		got, err := DeriveAddress(testXpub, index)
		if err != nil {
			t.Fatalf("DeriveAddress(index=%d) error = %v", index, err)
		}
		if got != wantAddr {
			t.Errorf("DeriveAddress(index=%d) = %q, want %q", index, got, wantAddr)
		}
		if seen[got] {
			t.Errorf("DeriveAddress(index=%d) = %q duplicates an address from another index", index, got)
		}
		seen[got] = true
	}
}

func TestDeriveAddress_InvalidXpub(t *testing.T) {
	if _, err := DeriveAddress("not-an-xpub", 0); err == nil {
		t.Fatal("DeriveAddress() error = nil, want error for malformed xpub")
	}
}

func TestDeriveAddress_RejectsPrivateKey(t *testing.T) {
	seed := make([]byte, hdkeychain.RecommendedSeedLen)
	if _, err := rand.Read(seed); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	master, err := hdkeychain.NewMaster(seed, &chaincfg.MainNetParams)
	if err != nil {
		t.Fatalf("hdkeychain.NewMaster: %v", err)
	}
	xprv := master.String()

	if _, err := DeriveAddress(xprv, 0); err == nil {
		t.Fatal("DeriveAddress() error = nil, want error when given a private extended key (xprv)")
	}
}

func TestDeriveAddress_RejectsHardenedIndex(t *testing.T) {
	if _, err := DeriveAddress(testXpub, hdkeychain.HardenedKeyStart); err == nil {
		t.Fatal("DeriveAddress() error = nil, want error for a hardened index on a public-only key")
	}
}
