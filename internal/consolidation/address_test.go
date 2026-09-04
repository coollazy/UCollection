package consolidation

import "testing"

func TestIsValidTronAddress(t *testing.T) {
	if !isValidTronAddress(testDestinationAddress) {
		t.Errorf("isValidTronAddress(%q) = false, want true", testDestinationAddress)
	}
	for _, bad := range []string{
		"",
		"not-an-address",
		"1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2", // valid Base58Check but Bitcoin's version byte, not Tron's 0x41
		testDestinationAddress[:len(testDestinationAddress)-1],
	} {
		if isValidTronAddress(bad) {
			t.Errorf("isValidTronAddress(%q) = true, want false", bad)
		}
	}
}

func TestAbiEncodeTransfer(t *testing.T) {
	// Real parameter hex manually verified against a live
	// triggersmartcontract call to Shasta during this module's development
	// (see docs/進度.md) — destination TSeJkUh4Qv67VNFwY8LaAxERygNdy6NQZK's
	// hex form b6e708a39781c96bd399c7657780ff9fe9f052a8, amount 1234567.
	const want = "000000000000000000000000b6e708a39781c96bd399c7657780ff9fe9f052a8000000000000000000000000000000000000000000000000000000000012d687"

	got, err := abiEncodeTransfer(testDestinationAddress, 1234567)
	if err != nil {
		t.Fatalf("abiEncodeTransfer() error = %v", err)
	}
	if got != want {
		t.Errorf("abiEncodeTransfer() = %s, want %s", got, want)
	}
	if len(got) != 128 {
		t.Errorf("len(abiEncodeTransfer()) = %d, want 128 (two 32-byte words hex-encoded)", len(got))
	}
}

func TestAbiEncodeTransfer_RejectsNonPositiveAmount(t *testing.T) {
	if _, err := abiEncodeTransfer(testDestinationAddress, 0); err == nil {
		t.Error("abiEncodeTransfer(amount=0) error = nil, want error")
	}
	if _, err := abiEncodeTransfer(testDestinationAddress, -1); err == nil {
		t.Error("abiEncodeTransfer(amount=-1) error = nil, want error")
	}
}

func TestAbiEncodeTransfer_RejectsInvalidDestination(t *testing.T) {
	if _, err := abiEncodeTransfer("not-an-address", 1); err == nil {
		t.Error("abiEncodeTransfer(bad destination) error = nil, want error")
	}
}
