package server

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
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

	actionBytes, err := sortedMapToMsgpack(action)
	require.NoError(t, err)

	messageBytes := make([]byte, 0, len(actionBytes)+8+20+8)
	messageBytes = append(messageBytes, actionBytes...)

	nonceBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBytes, uint64(nonce))
	messageBytes = append(messageBytes, nonceBytes...)

	messageBytes = append(messageBytes, make([]byte, 20)...)
	messageBytes = append(messageBytes, make([]byte, 8)...)

	hash := crypto.Keccak256Hash(messageBytes)
	sig, err := crypto.Sign(hash.Bytes(), key)
	require.NoError(t, err)

	return testSignature{
		R: "0x" + hex.EncodeToString(sig[:32]),
		S: "0x" + hex.EncodeToString(sig[32:64]),
		V: int(sig[64]) + 27,
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

	// Log raw JSON response
	t.Logf("Raw JSON response:\n%s", w.Body.String())

	// Parse response
	var response Meta
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v\nBody: %s", err, w.Body.String())
	}

	// Validate response structure
	if len(response.Universe) == 0 {
		t.Error("Expected universe to have assets")
	}

	// Check for expected assets
	foundBTC := false
	foundETH := false
	for _, asset := range response.Universe {
		if asset.Name == "BTC" {
			foundBTC = true
			if asset.SzDecimals != 5 {
				t.Errorf("BTC: expected szDecimals=5, got %d", asset.SzDecimals)
			}
			if asset.MaxLeverage == 0 {
				t.Error("BTC: expected MaxLeverage > 0")
			}
		}
		if asset.Name == "ETH" {
			foundETH = true
			if asset.SzDecimals != 4 {
				t.Errorf("ETH: expected szDecimals=4, got %d", asset.SzDecimals)
			}
		}
	}

	if !foundBTC {
		t.Error("Expected to find BTC in universe")
	}
	if !foundETH {
		t.Error("Expected to find ETH in universe")
	}

	// Check margin tables (they are tuples: [[id, {object}], ...])
	if len(response.MarginTables) == 0 {
		t.Error("Expected margin tables to exist")
	}

	for i, tuple := range response.MarginTables {
		if len(tuple) != 2 {
			t.Errorf("MarginTable %d: expected 2-element tuple, got %d elements", i, len(tuple))
			continue
		}

		// Extract id and table object from tuple
		// JSON unmarshals numbers as float64 when target is interface{}
		var id int
		switch v := tuple[0].(type) {
		case int:
			id = v
		case float64:
			id = int(v)
		default:
			t.Errorf("MarginTable %d: expected numeric id, got %T", i, tuple[0])
			continue
		}

		tableObj, ok := tuple[1].(map[string]interface{})
		if !ok {
			t.Errorf("MarginTable %d: expected map[string]interface{}, got %T", i, tuple[1])
			continue
		}

		// Check marginTiers exists in the table object
		marginTiers, ok := tableObj["marginTiers"]
		if !ok {
			t.Errorf("MarginTable %d (id=%d): missing marginTiers", i, id)
			continue
		}

		tiersSlice, ok := marginTiers.([]map[string]interface{})
		if !ok {
			t.Errorf("MarginTable %d (id=%d): marginTiers has wrong type %T", i, id, marginTiers)
			continue
		}

		if len(tiersSlice) == 0 {
			t.Errorf("MarginTable %d (id=%d): expected margin tiers", i, id)
		}
	}

	t.Logf("✓ Meta response: %d assets, %d margin tables", len(response.Universe), len(response.MarginTables))
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

	// Validate response structure
	if len(response.Tokens) == 0 {
		t.Error("Expected tokens to exist")
	}

	if len(response.Universe) == 0 {
		t.Error("Expected universe to have trading pairs")
	}

	// Check for expected tokens
	foundUSDC := false
	for _, token := range response.Tokens {
		if token.Name == "USDC" {
			foundUSDC = true
			if token.SzDecimals != 6 {
				t.Errorf("USDC: expected szDecimals=6, got %d", token.SzDecimals)
			}
		}
	}

	if !foundUSDC {
		t.Error("Expected to find USDC in tokens")
	}

	t.Logf("✓ SpotMeta response: %d tokens, %d trading pairs", len(response.Tokens), len(response.Universe))
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

	if len(response.Universe) == 0 {
		t.Error("Expected universe to have assets")
	}

	if len(response.AssetCtxs) == 0 {
		t.Error("Expected assetCtxs to exist")
	}

	t.Logf("✓ MetaAndAssetCtxs response: %d assets, %d contexts", len(response.Universe), len(response.AssetCtxs))
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
