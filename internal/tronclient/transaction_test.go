package tronclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Fixtures below are real responses captured against the live Shasta
// testnet during this module's development (see docs/進度.md) — not
// hand-assembled guesses.

const triggerSmartContractSuccessFixture = `{"transaction":{"raw_data":{"ref_block_bytes":"e5e0","ref_block_hash":"2c5c36c5fea2de8a","expiration":1788506022000,"contract":[{"parameter":{"value":{"owner_address":"TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH","contract_address":"TG3XXyExBkPp9nzdajDZsozEu4BkaSJozs","data":"a9059cbb000000000000000000000000b6e708a39781c96bd399c7657780ff9fe9f052a8000000000000000000000000000000000000000000000000000000000012d687"},"type_url":"type.googleapis.com/protocol.TriggerSmartContract"},"type":"TriggerSmartContract"}],"timestamp":1788505964581,"fee_limit":100000000},"raw_data_hex":"0a02e5e022082c5c36c5fea2de8a40f0c090da86345aae01081f12a9010a31747970652e676f6f676c65617069732e636f6d2f70726f746f636f6c2e54726967676572536d617274436f6e747261637412740a1541c8599111f29c1e1e061265b4af93ea1f274ad78a12154142a1e39aefa49290f2b3f9ed688d7cecf86cd6e02244a9059cbb000000000000000000000000b6e708a39781c96bd399c7657780ff9fe9f052a8000000000000000000000000000000000000000000000000000000000012d68770a5808dda8634900180c2d72f","txID":"e100b697f91c4c5067ffdbc5fdcc46bdd7f3eae4cf2aba61841b120766fa370c","visible":true},"result":{"result":true}}`

// Decodes to "class org.tron.json.JSONException : Unexpected character
// (...)" — a real hex-encoded error message.
const triggerSmartContractErrorFixture = `{"result":{"code":"OTHER_ERROR","message":"636c617373206f72672e74726f6e2e6a736f6e2e4a534f4e457863657074696f6e203a20556e6578706563746564206368617261637465722028273c272028636f646520363029293a2077617320657870656374696e6720636f6d6d6120746f207365706172617465204f626a65637420656e74726965730a206174205b536f757263653a20524544414354454420286053747265616d52656164466561747572652e494e434c5544455f534f555243455f494e5f4c4f434154494f4e602064697361626c6564293b206c696e653a20312c20636f6c756d6e3a203231355d"}}`

const createTransactionErrorFixture = `{"Error":"class org.tron.core.exception.ContractValidateException : Validate TransferContract error, balance is not sufficient."}`

const broadcastSigErrorFixture = `{"code":"SIGERROR","message":"Validate signature error: Signature size is 1","txid":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}`

const transactionInfoFoundFixture = `{"id": "e10afdcfa93cef133696b708b7c3761b3fd8b41106fdfe61b324d034f9de21b2","fee": 1649400,"blockNumber": 67881840,"blockTimeStamp": 1787889618000,"contractResult": ["0000000000000000000000000000000000000000000000000000000000000001"],"contract_address": "4142a1e39aefa49290f2b3f9ed688d7cecf86cd6e0","receipt": {"energy_usage": 1,"energy_fee": 1304400,"energy_usage_total": 13045,"net_fee": 345000,"result": "SUCCESS"},"log": [{"address": "42a1e39aefa49290f2b3f9ed688d7cecf86cd6e0","topics": ["ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef","000000000000000000000000c8599111f29c1e1e061265b4af93ea1f274ad78a","000000000000000000000000b6e708a39781c96bd399c7657780ff9fe9f052a8"],"data": "000000000000000000000000000000000000000000000000000000000012d687"}]}`

func jsonServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTriggerSmartContract_Success(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, triggerSmartContractSuccessFixture)
	c := NewClient(srv.URL, "")

	got, err := c.TriggerSmartContract(context.Background(), TriggerSmartContractParams{
		OwnerAddress:     "TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH",
		ContractAddress:  "TG3XXyExBkPp9nzdajDZsozEu4BkaSJozs",
		FunctionSelector: "transfer(address,uint256)",
		Parameter:        "000000000000000000000000b6e708a39781c96bd399c7657780ff9fe9f052a8000000000000000000000000000000000000000000000000000000000012d687",
	})
	if err != nil {
		t.Fatalf("TriggerSmartContract() error = %v", err)
	}
	if got.TxID != "e100b697f91c4c5067ffdbc5fdcc46bdd7f3eae4cf2aba61841b120766fa370c" {
		t.Errorf("TxID = %q", got.TxID)
	}
	var tx struct {
		Visible bool `json:"visible"`
	}
	if err := json.Unmarshal(got.Transaction, &tx); err != nil {
		t.Fatalf("Transaction is not valid JSON: %v", err)
	}
	if !tx.Visible {
		t.Error("Transaction.visible = false, want true")
	}
}

func TestTriggerSmartContract_ErrorResponse(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, triggerSmartContractErrorFixture)
	c := NewClient(srv.URL, "")

	_, err := c.TriggerSmartContract(context.Background(), TriggerSmartContractParams{OwnerAddress: "T..."})
	if err == nil {
		t.Fatal("TriggerSmartContract() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "OTHER_ERROR") {
		t.Errorf("error = %v, want it to include the TronGrid error code", err)
	}
	if !strings.Contains(err.Error(), "JSONException") {
		t.Errorf("error = %v, want the hex-encoded message decoded to readable text", err)
	}
}

func TestTriggerSmartContract_RetriesOn5xx(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts <= 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(triggerSmartContractSuccessFixture))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	if _, err := c.TriggerSmartContract(context.Background(), TriggerSmartContractParams{}); err != nil {
		t.Fatalf("TriggerSmartContract() error = %v", err)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2 (prepare calls are safe to retry)", attempts)
	}
}

func TestCreateTransaction_ErrorResponse(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, createTransactionErrorFixture)
	c := NewClient(srv.URL, "")

	_, err := c.CreateTransaction(context.Background(), CreateTransactionParams{OwnerAddress: "T...", ToAddress: "T...", Amount: 1})
	if err == nil {
		t.Fatal("CreateTransaction() error = nil, want error")
	}
	// createtransaction's error field is plain text, not hex — must show up
	// verbatim, not garbled by an incorrect hex-decode attempt.
	if !strings.Contains(err.Error(), "balance is not sufficient") {
		t.Errorf("error = %v, want the plain-text Error message verbatim", err)
	}
}

func TestBroadcastTransaction_ExplicitFailureNotTreatedAsNetworkFailure(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, broadcastSigErrorFixture)
	c := NewClient(srv.URL, "")

	result, err := c.BroadcastTransaction(context.Background(), json.RawMessage(`{"txID":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}`))
	if err != nil {
		t.Fatalf("BroadcastTransaction() error = %v, want nil (TronGrid DID respond, just rejected it)", err)
	}
	if result.Success {
		t.Error("Success = true, want false")
	}
	if result.Code != "SIGERROR" {
		t.Errorf("Code = %q, want SIGERROR", result.Code)
	}
	// This real response's "message" is plain text, not hex — decodeMaybeHex
	// must not mangle it.
	if !strings.Contains(result.Message, "Signature size is 1") {
		t.Errorf("Message = %q, want the plain-text message verbatim", result.Message)
	}
	if errors.Is(err, ErrBroadcastNetworkFailure) {
		t.Error("an explicit rejection response must not be classified as ErrBroadcastNetworkFailure")
	}
}

func TestBroadcastTransaction_SuccessResponse(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, `{"result":true,"txid":"98cbb038f003708ee458cc3b3493719bf61caf3da6711633de4ba41b89dc65d9"}`)
	c := NewClient(srv.URL, "")

	result, err := c.BroadcastTransaction(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("BroadcastTransaction() error = %v", err)
	}
	if !result.Success {
		t.Error("Success = false, want true")
	}
	if result.TxID != "98cbb038f003708ee458cc3b3493719bf61caf3da6711633de4ba41b89dc65d9" {
		t.Errorf("TxID = %q", result.TxID)
	}
}

func TestBroadcastTransaction_NetworkFailureNeverRetries(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError) // would be retried by doPostRequest, but broadcast must not go through it
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	_, err := c.BroadcastTransaction(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("BroadcastTransaction() error = nil, want error for a 500 response")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want exactly 1 — BroadcastTransaction must never auto-retry", attempts)
	}
}

func TestBroadcastTransaction_TrueNetworkFailureIsErrBroadcastNetworkFailure(t *testing.T) {
	// A server that accepts the connection then closes it mid-response
	// simulates a genuine network-layer failure (no usable HTTP response).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("ResponseWriter does not support hijacking")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		_ = conn.Close()
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	_, err := c.BroadcastTransaction(context.Background(), json.RawMessage(`{}`))
	if !errors.Is(err, ErrBroadcastNetworkFailure) {
		t.Errorf("error = %v, want ErrBroadcastNetworkFailure for a true network-layer failure", err)
	}
}

func TestTransactionInfo_Found(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, transactionInfoFoundFixture)
	c := NewClient(srv.URL, "")

	got, err := c.TransactionInfo(context.Background(), "e10afdcfa93cef133696b708b7c3761b3fd8b41106fdfe61b324d034f9de21b2")
	if err != nil {
		t.Fatalf("TransactionInfo() error = %v", err)
	}
	if !got.Found {
		t.Fatal("Found = false, want true")
	}
	if got.BlockNumber != 67881840 {
		t.Errorf("BlockNumber = %d", got.BlockNumber)
	}
	if got.Receipt.Result != "SUCCESS" {
		t.Errorf("Receipt.Result = %q, want SUCCESS", got.Receipt.Result)
	}
	if len(got.Log) != 1 || got.Log[0].Data != "000000000000000000000000000000000000000000000000000000000012d687" {
		t.Errorf("Log = %+v", got.Log)
	}
}

func TestTransactionInfo_NotFound(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, `{}`)
	c := NewClient(srv.URL, "")

	got, err := c.TransactionInfo(context.Background(), "0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("TransactionInfo() error = %v", err)
	}
	if got.Found {
		t.Error("Found = true, want false for an empty {} response")
	}
}

func TestTransactionInfo_TransferContractHasNoReceiptResult(t *testing.T) {
	// Real shape recorded in 驗證結論-06 附加測試: TransferContract has no
	// receipt.result field at all.
	srv := jsonServer(t, http.StatusOK, `{"id":"c4b56bb64f2e19e26412a38c1c0cf1374eaa346bc3f433898ccbd8d216c01d07","blockNumber":67927794,"receipt":{"net_usage":268,"net_fee":268000}}`)
	c := NewClient(srv.URL, "")

	got, err := c.TransactionInfo(context.Background(), "c4b56bb64f2e19e26412a38c1c0cf1374eaa346bc3f433898ccbd8d216c01d07")
	if err != nil {
		t.Fatalf("TransactionInfo() error = %v", err)
	}
	if !got.Found || got.BlockNumber != 67927794 {
		t.Fatalf("got = %+v", got)
	}
	if got.Receipt.Result != "" {
		t.Errorf("Receipt.Result = %q, want empty for a TransferContract with no result field", got.Receipt.Result)
	}
}
