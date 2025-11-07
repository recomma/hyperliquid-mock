package server_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/recomma/hyperliquid-mock/server"
)

// Example demonstrating WebSocket order updates subscription
func ExampleWebSocketOrderUpdates(t *testing.T) {
	ts := server.NewTestServer(t)
	defer ts.Close()

	// Connect to WebSocket
	wsURL := ts.WebSocketURL()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Subscribe to order updates
	subscription := map[string]interface{}{
		"method": "subscribe",
		"subscription": map[string]interface{}{
			"type": "orderUpdates",
			"user": "0x1234567890abcdef",
		},
	}

	if err := conn.WriteJSON(subscription); err != nil {
		t.Fatalf("Failed to send subscription: %v", err)
	}

	// Read subscription acknowledgment
	var ack map[string]interface{}
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatalf("Failed to read ack: %v", err)
	}

	fmt.Printf("Subscription acknowledged: %s\n", ack["channel"])

	// Create an order (this will trigger a WebSocket update)
	// Note: In a real test, you'd use the HTTP API to create the order
	// For this example, we're just demonstrating the WebSocket flow

	fmt.Println("WebSocket orderUpdates subscription working!")
}

// Example demonstrating WebSocket BBO subscription
func ExampleWebSocketBBO(t *testing.T) {
	ts := server.NewTestServer(t)
	defer ts.Close()

	// Connect to WebSocket
	wsURL := ts.WebSocketURL()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Subscribe to BBO for BTC
	subscription := map[string]interface{}{
		"method": "subscribe",
		"subscription": map[string]interface{}{
			"type": "l2Book",
			"coin": "BTC",
		},
	}

	if err := conn.WriteJSON(subscription); err != nil {
		t.Fatalf("Failed to send subscription: %v", err)
	}

	// Read subscription acknowledgment
	var ack map[string]interface{}
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatalf("Failed to read ack: %v", err)
	}

	fmt.Printf("Subscription acknowledged: %s\n", ack["channel"])

	// Read initial BBO snapshot
	var bboMsg map[string]interface{}
	if err := conn.ReadJSON(&bboMsg); err != nil {
		t.Fatalf("Failed to read BBO: %v", err)
	}

	if bboMsg["channel"] == "l2Book" {
		data := bboMsg["data"].(map[string]interface{})
		fmt.Printf("Received BBO for %s\n", data["coin"])

		levels := data["levels"].([]interface{})
		bids := levels[0].([]interface{})
		asks := levels[1].([]interface{})

		if len(bids) > 0 {
			bid := bids[0].(map[string]interface{})
			fmt.Printf("Best bid: %s @ %s\n", bid["sz"], bid["px"])
		}
		if len(asks) > 0 {
			ask := asks[0].(map[string]interface{})
			fmt.Printf("Best ask: %s @ %s\n", ask["sz"], ask["px"])
		}
	}

	fmt.Println("WebSocket BBO subscription working!")
}

// Example demonstrating manual BBO updates
func ExampleManualBBOUpdate(t *testing.T) {
	ts := server.NewTestServer(t)
	defer ts.Close()

	// Connect to WebSocket
	wsURL := ts.WebSocketURL()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Subscribe to BBO for BTC
	subscription := map[string]interface{}{
		"method": "subscribe",
		"subscription": map[string]interface{}{
			"type": "l2Book",
			"coin": "BTC",
		},
	}

	if err := conn.WriteJSON(subscription); err != nil {
		t.Fatalf("Failed to send subscription: %v", err)
	}

	// Read subscription acknowledgment
	conn.ReadJSON(&map[string]interface{}{})

	// Read initial BBO snapshot
	conn.ReadJSON(&map[string]interface{}{})

	// Manually set BBO prices
	ts.SetBBO("BTC", 87000.0, 5.0, 87100.0, 4.5)

	// Give it a moment to broadcast
	time.Sleep(50 * time.Millisecond)

	// Read the updated BBO
	var bboUpdate map[string]interface{}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := conn.ReadJSON(&bboUpdate); err != nil {
		t.Fatalf("Failed to read BBO update: %v", err)
	}

	if bboUpdate["channel"] == "l2Book" {
		data := bboUpdate["data"].(map[string]interface{})
		levels := data["levels"].([]interface{})
		bids := levels[0].([]interface{})

		if len(bids) > 0 {
			bid := bids[0].(map[string]interface{})
			fmt.Printf("Updated bid price: %s\n", bid["px"])
		}
	}

	fmt.Println("Manual BBO update working!")
}

// Helper function to pretty print JSON
func prettyPrint(v interface{}) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}
