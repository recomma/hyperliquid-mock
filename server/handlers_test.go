package server

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	hyperliquid "github.com/sonirico/go-hyperliquid"
	"github.com/stretchr/testify/require"
)

const sampleOrderActionJSON = `{
  "grouping": "na",
  "type": "order",
  "builder": { "b": "mock-builder", "f": 1 },
  "orders": [
    {
      "a": 1,
      "b": true,
      "p": "3200.5",
      "s": "0.75",
      "r": false,
      "c": "00000000000000000000000000000001",
      "t": {
        "limit": { "tif": "GTC" }
      }
    }
  ]
}`

// TestRecoverWalletFromSignature ensures we can recover the wallet address for a
// signed request using the deterministic msgpack encoding path.
func TestRecoverWalletFromSignature(t *testing.T) {
	handler := NewHandler()

	privKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	expectedWallet := crypto.PubkeyToAddress(privKey.PublicKey).Hex()

	var action map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(sampleOrderActionJSON), &action))

	const nonce int64 = 42

	signature := signActionForTest(t, privKey, action, nonce)
	req := &ExchangeRequest{
		Action: action,
		Nonce:  nonce,
	}
	req.Signature.R = signature.R
	req.Signature.S = signature.S
	req.Signature.V = signature.V

	wallet, err := handler.recoverWalletFromSignature(req)
	require.NoError(t, err)
	require.Equal(t, expectedWallet, wallet)
}

type testSignature struct {
	R string
	S string
	V int
}

func signActionForTest(t *testing.T, key *ecdsa.PrivateKey, action map[string]interface{}, nonce int64) testSignature {
	t.Helper()

	body, err := json.Marshal(action)
	require.NoError(t, err)

	var typed hyperliquid.OrderAction
	require.NoError(t, json.Unmarshal(body, &typed))

	sig, err := hyperliquid.SignL1Action(key, typed, "", nonce, nil, false)
	require.NoError(t, err)

	return testSignature{
		R: sig.R,
		S: sig.S,
		V: sig.V,
	}
}

// TestHandleInfo_Meta tests the /info endpoint with type "meta"
func TestHandleInfo_Meta(t *testing.T) {
	handler := NewHandler()

	// Create request
	reqBody := InfoRequest{
		Type: "meta",
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/info", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Execute request
	handler.HandleInfo(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
		t.Logf("Response body: %s", w.Body.String())
		return
	}

	// Parse response
	var response Meta
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v\nBody: %s", err, w.Body.String())
	}

	if got, want := len(response.Universe), 203; got != want {
		t.Fatalf("expected %d assets in universe, got %d", want, got)
	}
	if got, want := len(response.MarginTables), 7; got != want {
		t.Fatalf("expected %d margin tables, got %d", want, got)
	}

	btc := response.Universe[0]
	if btc.Name != "BTC" || btc.SzDecimals != 5 || btc.MaxLeverage != 40 || btc.MarginTableId != 56 {
		t.Fatalf("unexpected BTC metadata: %+v", btc)
	}

	last := response.Universe[len(response.Universe)-1]
	if last.Name == "" {
		t.Fatalf("expected last universe entry to have a name: %+v", last)
	}

	t.Logf("✓ Meta response matches recorded fixture: %d assets", len(response.Universe))
}

func TestProcessOrderRejectsLowPriceBtcIOC(t *testing.T) {
	handler := NewHandler()

	orderMap := map[string]interface{}{
		"a": float64(0),
		"b": true,
		"s": "1",
		"p": "100",
		"t": map[string]interface{}{
			"limit": map[string]interface{}{
				"tif": "Ioc",
			},
		},
	}

	status := handler.processOrder(orderMap, "0xwallet1")

	if status.Resting != nil {
		t.Fatalf("expected no resting order, got %#v", status.Resting)
	}

	if status.Error == nil {
		t.Fatalf("expected error %q, got nil", ErrOrderIocCancel.Error())
	}

	if got, want := *status.Error, ErrOrderIocCancel.Error(); got != want {
		t.Fatalf("unexpected error message: got %q want %q", got, want)
	}
}

func TestProcessOrderAllowsOtherIocOrders(t *testing.T) {
	handler := NewHandler()

	// we're simulating a BTC order, however it demands a price above 87000 USDT
	orderMap := map[string]interface{}{
		"a": float64(0),
		"b": true,
		"s": "0.01",
		"p": "87001",
		"t": map[string]interface{}{
			"limit": map[string]interface{}{
				"tif": "Ioc",
			},
		},
	}

	status := handler.processOrder(orderMap, "0xwallet1")

	if status.Error != nil {
		t.Fatalf("expected no error, got %q", *status.Error)
	}

	if status.Resting == nil {
		t.Fatalf("expected resting order, got nil")
	}
}

// TestHandleInfo_SpotMeta tests the /info endpoint with type "spotMeta"
func TestHandleInfo_SpotMeta(t *testing.T) {
	handler := NewHandler()

	// Create request
	reqBody := InfoRequest{
		Type: "spotMeta",
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/info", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Execute request
	handler.HandleInfo(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
		t.Logf("Response body: %s", w.Body.String())
		return
	}

	// Parse response
	var response SpotMeta
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v\nBody: %s", err, w.Body.String())
	}

	if got, want := len(response.Tokens), 342; got != want {
		t.Fatalf("expected %d spot tokens, got %d", want, got)
	}
	if got, want := len(response.Universe), 199; got != want {
		t.Fatalf("expected %d spot pairs, got %d", want, got)
	}

	firstToken := response.Tokens[0]
	if firstToken.Name != "USDC" || firstToken.SzDecimals != 8 {
		t.Fatalf("unexpected first token %+v", firstToken)
	}

	firstPair := response.Universe[0]
	if firstPair.Name != "PURR/USDC" || firstPair.Index != 0 {
		t.Fatalf("unexpected first universe entry %+v", firstPair)
	}

	t.Logf("✓ SpotMeta response matches recorded fixture: %d tokens, %d trading pairs", len(response.Tokens), len(response.Universe))
}

// TestHandleInfo_SpotMetaAndAssetCtxs validates the combined spot metadata and asset contexts
func TestHandleInfo_SpotMetaAndAssetCtxs(t *testing.T) {
	handler := NewHandler()

	reqBody := InfoRequest{Type: "spotMetaAndAssetCtxs"}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/info", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleInfo(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var response SpotMetaAndAssetCtxs
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v (body: %s)", err, w.Body.String())
	}

	if got, want := len(response.Tokens), 342; got != want {
		t.Fatalf("expected %d tokens, got %d", want, got)
	}
	if got, want := len(response.Universe), 199; got != want {
		t.Fatalf("expected %d universe entries, got %d", want, got)
	}
	if got, want := len(response.AssetCtxs), 210; got != want {
		t.Fatalf("expected %d asset ctx entries, got %d", want, got)
	}

	firstCtx := response.AssetCtxs[0]
	if firstCtx.Coin != "PURR/USDC" || firstCtx.MarkPx == "" || firstCtx.DayNtlVlm == "" {
		t.Fatalf("unexpected first asset ctx %+v", firstCtx)
	}
}

// TestHandleInfo_MetaAndAssetCtxs tests the existing metaAndAssetCtxs endpoint
func TestHandleInfo_MetaAndAssetCtxs(t *testing.T) {
	handler := NewHandler()

	reqBody := InfoRequest{
		Type: "metaAndAssetCtxs",
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/info", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleInfo(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
		t.Logf("Response body: %s", w.Body.String())
		return
	}

	var response MetaAndAssetCtxs
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v\nBody: %s", err, w.Body.String())
	}

	if got, want := len(response.Universe), 203; got != want {
		t.Fatalf("expected %d universe entries, got %d", want, got)
	}
	if got, want := len(response.AssetCtxs), 203; got != want {
		t.Fatalf("expected %d asset contexts, got %d", want, got)
	}
	if got, want := len(response.MarginTables), 7; got != want {
		t.Fatalf("expected %d margin tables, got %d", want, got)
	}

	firstCtx := response.AssetCtxs[0]
	if firstCtx.MarkPx == "" || firstCtx.Funding == "" {
		t.Fatalf("unexpected first asset context: %+v", firstCtx)
	}

	t.Logf("✓ MetaAndAssetCtxs response matches recorded fixture: %d assets, %d contexts", len(response.Universe), len(response.AssetCtxs))
}

// TestHandleInfo_UnknownType tests that unknown info types return 400
func TestHandleInfo_UnknownType(t *testing.T) {
	handler := NewHandler()

	reqBody := InfoRequest{
		Type: "nonExistentType",
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/info", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleInfo(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for unknown type, got %d", w.Code)
	}
}
