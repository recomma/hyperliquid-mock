package server_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/recomma/hyperliquid-mock/server"
)

// TestServerIsolation demonstrates that each test gets its own isolated server
func TestServerIsolation(t *testing.T) {
	// Test 1: Create first server
	ts1 := server.NewTestServer(t)

	// Make a request to server 1
	makeExchangeRequest(t, ts1.URL(), "ETH", 1.0, 3000.0)

	// Test 2: Create second server
	ts2 := server.NewTestServer(t)

	// Server 2 should have no requests
	if ts2.RequestCount() != 0 {
		t.Errorf("Expected ts2 to have 0 requests, got %d", ts2.RequestCount())
	}

	// Server 1 should have 1 request
	if ts1.RequestCount() != 1 {
		t.Errorf("Expected ts1 to have 1 request, got %d", ts1.RequestCount())
	}
}

// TestCaptureExchangeRequest demonstrates capturing and inspecting exchange requests
func TestCaptureExchangeRequest(t *testing.T) {
	ts := server.NewTestServer(t)

	// Make an order request
	makeExchangeRequest(t, ts.URL(), "BTC", 0.5, 50000.0)

	// Get captured requests
	requests := ts.GetExchangeRequests()
	if len(requests) != 1 {
		t.Fatalf("Expected 1 exchange request, got %d", len(requests))
	}

	// Inspect the request
	req := requests[0]
	if req.Nonce != 123456789 {
		t.Errorf("Expected nonce 123456789, got %d", req.Nonce)
	}

	// Inspect the action payload
	actionMap, ok := req.Action.(map[string]interface{})
	if !ok {
		t.Fatal("Expected action to be a map")
	}

	orders, ok := actionMap["orders"].([]interface{})
	if !ok || len(orders) == 0 {
		t.Fatal("Expected orders array in action")
	}

	order := orders[0].(map[string]interface{})
	if order["coin"] != "BTC" {
		t.Errorf("Expected coin BTC, got %v", order["coin"])
	}
}

// TestCaptureInfoRequest demonstrates capturing info queries
func TestCaptureInfoRequest(t *testing.T) {
	ts := server.NewTestServer(t)

	// Query metadata
	makeInfoRequest(t, ts.URL(), "metaAndAssetCtxs")

	// Get captured info requests
	requests := ts.GetInfoRequests()
	if len(requests) != 1 {
		t.Fatalf("Expected 1 info request, got %d", len(requests))
	}

	if requests[0].Type != "metaAndAssetCtxs" {
		t.Errorf("Expected type metaAndAssetCtxs, got %s", requests[0].Type)
	}
}

// TestOrderStateInspection demonstrates inspecting server's order state
func TestOrderStateInspection(t *testing.T) {
	ts := server.NewTestServer(t)

	// Create an order
	makeExchangeRequest(t, ts.URL(), "SOL", 10.0, 100.0)

	// Wait a bit for processing
	time.Sleep(10 * time.Millisecond)

	// Get the exchange request to find the CLOID
	exchangeReqs := ts.GetExchangeRequests()
	if len(exchangeReqs) == 0 {
		t.Fatal("No exchange requests captured")
	}

	// Extract CLOID from the request
	actionMap := exchangeReqs[0].Action.(map[string]interface{})
	orders := actionMap["orders"].([]interface{})
	order := orders[0].(map[string]interface{})
	cloid := order["cloid"].(string)

	// Check if order exists in state
	storedOrder, exists := ts.GetOrder(cloid)
	if !exists {
		t.Fatal("Order not found in server state")
	}

	if storedOrder.Order.Coin != "SOL" {
		t.Errorf("Expected coin SOL, got %s", storedOrder.Order.Coin)
	}

	if storedOrder.Status != "open" {
		t.Errorf("Expected status open, got %s", storedOrder.Status)
	}
}

// TestClearRequestHistory demonstrates clearing request history between test phases
func TestClearRequestHistory(t *testing.T) {
	ts := server.NewTestServer(t)

	// Phase 1: Make some requests
	makeExchangeRequest(t, ts.URL(), "ETH", 1.0, 3000.0)
	makeInfoRequest(t, ts.URL(), "metaAndAssetCtxs")

	if ts.RequestCount() != 2 {
		t.Errorf("Expected 2 requests, got %d", ts.RequestCount())
	}

	// Clear history
	ts.ClearRequests()

	if ts.RequestCount() != 0 {
		t.Errorf("Expected 0 requests after clear, got %d", ts.RequestCount())
	}

	// Phase 2: Make new requests
	makeExchangeRequest(t, ts.URL(), "BTC", 0.1, 50000.0)

	if ts.RequestCount() != 1 {
		t.Errorf("Expected 1 request after clear, got %d", ts.RequestCount())
	}
}

// TestMultipleOrders demonstrates testing multiple order operations
func TestMultipleOrders(t *testing.T) {
	ts := server.NewTestServer(t)

	// Create multiple orders
	coins := []string{"BTC", "ETH", "SOL", "ARB"}
	for _, coin := range coins {
		makeExchangeRequest(t, ts.URL(), coin, 1.0, 1000.0)
	}

	// Verify all requests were captured
	if ts.RequestCount() != len(coins) {
		t.Errorf("Expected %d requests, got %d", len(coins), ts.RequestCount())
	}

	// Verify all exchange requests
	exchangeReqs := ts.GetExchangeRequests()
	if len(exchangeReqs) != len(coins) {
		t.Errorf("Expected %d exchange requests, got %d", len(coins), len(exchangeReqs))
	}
}

func TestTestServerFillOrder(t *testing.T) {
	t.Run("full fill", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
		ts := server.NewTestServer(t, server.WithLogger(logger))

		makeExchangeRequest(t, ts.URL(), "BTC", 1.0, 50000.0)
		cloid := extractCloid(t, ts)

		if err := ts.FillOrder(cloid, 50500.5); err != nil {
			t.Fatalf("FillOrder returned error: %v", err)
		}

		order, exists := ts.GetOrder(cloid)
		if !exists {
			t.Fatal("order not found after creation")
		}

		result := queryOrderStatus(t, ts.URL(), order.Order.User, cloid)

		if result.Status != "order" {
			t.Fatalf("expected order status response, got %s", result.Status)
		}
		if result.Order == nil {
			t.Fatal("expected order detail in response")
		}
		if result.Order.Status != "filled" {
			t.Fatalf("expected filled status, got %s", result.Order.Status)
		}
		if result.Order.Order.Sz != "0" {
			t.Fatalf("expected size 0, got %s", result.Order.Order.Sz)
		}
		if result.Order.Order.OrigSz != "1" {
			t.Fatalf("expected orig size 1, got %s", result.Order.Order.OrigSz)
		}
		if result.Order.Order.LimitPx != "50500.5" {
			t.Fatalf("expected limit px 50500.5, got %s", result.Order.Order.LimitPx)
		}
		if result.Order.StatusTimestamp == 0 {
			t.Fatal("expected status timestamp to be set")
		}
	})

	t.Run("partial fill", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
		ts := server.NewTestServer(t, server.WithLogger(logger))

		makeExchangeRequest(t, ts.URL(), "ETH", 1.0, 3000.0)
		cloid := extractCloid(t, ts)

		if err := ts.FillOrder(cloid, 2999.25, server.WithFillSize(0.4)); err != nil {
			t.Fatalf("FillOrder returned error: %v", err)
		}

		order, exists := ts.GetOrder(cloid)
		if !exists {
			t.Fatal("order not found after creation")
		}

		result := queryOrderStatus(t, ts.URL(), order.Order.User, cloid)

		if result.Status != "order" {
			t.Fatalf("expected order status response, got %s", result.Status)
		}
		if result.Order == nil {
			t.Fatal("expected order detail in response")
		}
		if result.Order.Status != "open" {
			t.Fatalf("expected open status after partial fill, got %s", result.Order.Status)
		}
		if result.Order.Order.Sz != "0.6" {
			t.Fatalf("expected remaining size 0.6, got %s", result.Order.Order.Sz)
		}
		if result.Order.Order.OrigSz != "1" {
			t.Fatalf("expected orig size 1, got %s", result.Order.Order.OrigSz)
		}
		if result.Order.Order.LimitPx != "2999.25" {
			t.Fatalf("expected limit px 2999.25, got %s", result.Order.Order.LimitPx)
		}
		if result.Order.StatusTimestamp == 0 {
			t.Fatal("expected status timestamp to be set")
		}
	})
}

// TestRawRequestInspection demonstrates low-level request inspection
func TestRawRequestInspection(t *testing.T) {
	ts := server.NewTestServer(t)

	// Make a request
	makeExchangeRequest(t, ts.URL(), "ETH", 1.0, 3000.0)

	// Get raw captured requests
	rawRequests := ts.GetRequests()
	if len(rawRequests) != 1 {
		t.Fatalf("Expected 1 raw request, got %d", len(rawRequests))
	}

	req := rawRequests[0]

	// Inspect raw details
	if req.Method != "POST" {
		t.Errorf("Expected POST method, got %s", req.Method)
	}

	if req.Path != "/exchange" {
		t.Errorf("Expected /exchange path, got %s", req.Path)
	}

	contentType := req.Headers.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	// Verify timestamp is recent
	if time.Since(req.Timestamp) > time.Second {
		t.Error("Request timestamp is too old")
	}
}

// Helper function to make an exchange request
func makeExchangeRequest(t *testing.T, baseURL, coin string, size, price float64) {
	t.Helper()

	payload := map[string]interface{}{
		"action": map[string]interface{}{
			"type": "order",
			"orders": []map[string]interface{}{
				{
					"coin":     coin,
					"is_buy":   true,
					"sz":       size,
					"limit_px": price,
					"cloid":    "test-" + coin + "-" + time.Now().Format("20060102150405"),
					"order_type": map[string]interface{}{
						"limit": map[string]interface{}{
							"tif": "Gtc",
						},
					},
				},
			},
		},
		"nonce": 123456789,
		"signature": map[string]interface{}{
			"r": "0x1234",
			"s": "0x5678",
			"v": 27,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	resp, err := http.Post(baseURL+"/exchange", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

// Helper function to make an info request
func makeInfoRequest(t *testing.T, baseURL, reqType string) {
	t.Helper()

	payload := map[string]interface{}{
		"type": reqType,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	resp, err := http.Post(baseURL+"/info", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func queryOrderStatus(t *testing.T, baseURL, user, cloid string) server.OrderQueryResult {
	t.Helper()

	payload := map[string]interface{}{
		"type": "orderStatus",
		"oid":  cloid,
	}
	if user != "" {
		payload["user"] = user
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	resp, err := http.Post(baseURL+"/info", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var result server.OrderQueryResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	return result
}

func extractCloid(t *testing.T, ts *server.TestServer) string {
	t.Helper()

	exchangeReqs := ts.GetExchangeRequests()
	if len(exchangeReqs) == 0 {
		t.Fatal("No exchange requests captured")
	}

	actionMap, ok := exchangeReqs[len(exchangeReqs)-1].Action.(map[string]interface{})
	if !ok {
		t.Fatal("Expected action to be a map")
	}

	orders, ok := actionMap["orders"].([]interface{})
	if !ok || len(orders) == 0 {
		t.Fatal("Expected orders array in action")
	}

	order, ok := orders[0].(map[string]interface{})
	if !ok {
		t.Fatal("Expected order to be a map")
	}

	cloid, ok := order["cloid"].(string)
	if !ok {
		t.Fatal("Expected cloid to be a string")
	}

	return cloid
}
