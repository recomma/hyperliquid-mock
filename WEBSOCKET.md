# WebSocket Implementation

This document describes the WebSocket functionality added to the Hyperliquid Mock Server.

## Overview

The mock server now supports WebSocket connections for real-time data streaming, matching the Hyperliquid API protocol. Two primary subscription types are implemented:

1. **Order Updates** (`orderUpdates`) - Real-time order status updates
2. **Level 2 Book / BBO** (`l2Book` / `bbo`) - Real-time market data

## WebSocket Endpoint

- **URL**: `ws://localhost:<port>/ws`
- **Protocol**: WebSocket (RFC 6455)
- **Connection**: Persistent, bidirectional

## Subscription Types

### 1. Order Updates Subscription

Receive real-time updates when orders are created, modified, filled, or canceled.

#### Subscription Request
```json
{
  "method": "subscribe",
  "subscription": {
    "type": "orderUpdates",
    "user": "0xWALLET_ADDRESS"
  }
}
```

#### Update Message Format
```json
{
  "channel": "orderUpdates",
  "data": [
    {
      "order": {
        "coin": "BTC",
        "side": "B",
        "limitPx": "87000",
        "sz": "1.0",
        "oid": 1000001,
        "timestamp": 1699999999999,
        "origSz": "1.0",
        "cloid": "000000010000000200000003"
      },
      "status": "open",
      "statusTimestamp": 1699999999999
    }
  ]
}
```

#### Order Status Values
- `open` - Order is active on the order book
- `filled` - Order is completely filled
- `canceled` - Order has been canceled

#### Trigger Events
Updates are automatically sent when:
- Order created → `status: "open"`
- Order modified → `status: "open"` (with updated price/size)
- Order filled (full or partial) → `status: "filled"` or `status: "open"` with updated `sz`
- Order canceled → `status: "canceled"`

### 2. BBO (Best Bid Offer) Subscription

Receive real-time market data for price discovery.

#### Subscription Request (l2Book)
```json
{
  "method": "subscribe",
  "subscription": {
    "type": "l2Book",
    "coin": "BTC"
  }
}
```

#### Subscription Request (bbo alternative)
```json
{
  "method": "subscribe",
  "subscription": {
    "type": "bbo",
    "coin": "BTC"
  }
}
```

#### Update Message Format
```json
{
  "channel": "l2Book",
  "data": {
    "coin": "BTC",
    "time": 1699999999999,
    "levels": [
      [
        {"px": "86956.5", "sz": "10.5", "n": 1}
      ],
      [
        {"px": "87043.5", "sz": "8.3", "n": 1}
      ]
    ]
  }
}
```

#### Level Fields
- `px` - Price as string
- `sz` - Size/quantity as string
- `n` - Number of orders at this price level

#### Default BBO Prices

The mock server provides realistic default prices:

| Coin | Bid Price | Ask Price | Spread |
|------|-----------|-----------|--------|
| BTC  | 86956.5   | 87043.5   | ~0.1%  |
| ETH  | 2999.5    | 3000.5    | ~0.03% |
| SOL  | 99.9      | 100.1     | ~0.2%  |

Note: BTC prices are centered around 87000 USDT to match the existing IOC test behavior.

## Connection Management

### Establishing Connection

```javascript
const ws = new WebSocket('ws://localhost:8080/ws');

ws.onopen = () => {
  console.log('Connected to Hyperliquid Mock Server');

  // Subscribe to order updates
  ws.send(JSON.stringify({
    method: 'subscribe',
    subscription: {
      type: 'orderUpdates',
      user: '0x1234567890abcdef'
    }
  }));
};

ws.onmessage = (event) => {
  const message = JSON.parse(event.data);
  console.log('Received:', message);
};
```

### Subscription Acknowledgment

After sending a subscription request, the server responds with:

```json
{
  "channel": "subscriptionResponse",
  "data": {
    "method": "subscribe",
    "subscription": {
      "type": "orderUpdates",
      "user": "0x1234567890abcdef"
    }
  }
}
```

### Unsubscribing

```json
{
  "method": "unsubscribe",
  "subscription": {
    "type": "orderUpdates",
    "user": "0xWALLET_ADDRESS"
  }
}
```

### Multiple Subscriptions

A single WebSocket connection can maintain multiple subscriptions:

```javascript
// Subscribe to order updates
ws.send(JSON.stringify({
  method: 'subscribe',
  subscription: { type: 'orderUpdates', user: '0x123...' }
}));

// Subscribe to BTC BBO
ws.send(JSON.stringify({
  method: 'subscribe',
  subscription: { type: 'l2Book', coin: 'BTC' }
}));

// Subscribe to ETH BBO
ws.send(JSON.stringify({
  method: 'subscribe',
  subscription: { type: 'l2Book', coin: 'ETH' }
}));
```

## Test Server API

The `TestServer` includes helper methods for WebSocket testing:

### Get WebSocket URL

```go
ts := server.NewTestServer(t)
wsURL := ts.WebSocketURL() // Returns "ws://127.0.0.1:12345/ws"
```

### Manual BBO Updates

```go
// Set specific BBO prices
ts.SetBBO("BTC", 87000.0, 5.0, 87100.0, 4.5)
//            coin   bidPx  bidSz  askPx   askSz

// Trigger a BBO update with default prices
ts.TriggerBBOUpdate("BTC")
```

### Example Test

```go
func TestWebSocketOrderUpdates(t *testing.T) {
    ts := server.NewTestServer(t)
    defer ts.Close()

    // Connect to WebSocket
    conn, _, err := websocket.DefaultDialer.Dial(ts.WebSocketURL(), nil)
    require.NoError(t, err)
    defer conn.Close()

    // Subscribe
    subscription := map[string]interface{}{
        "method": "subscribe",
        "subscription": map[string]interface{}{
            "type": "orderUpdates",
            "user": "0x1234",
        },
    }
    conn.WriteJSON(subscription)

    // Read subscription ack
    var ack map[string]interface{}
    conn.ReadJSON(&ack)

    // Place an order via HTTP API (triggers WebSocket update)
    // ... HTTP request ...

    // Receive order update
    var update map[string]interface{}
    conn.ReadJSON(&update)

    assert.Equal(t, "orderUpdates", update["channel"])
}
```

## Error Handling

### Error Message Format

```json
{
  "channel": "error",
  "error": "Invalid subscription type"
}
```

### Common Errors

- `"Invalid subscription message"` - Malformed JSON
- `"Unknown method: <method>"` - Invalid method field
- `"Missing subscription type"` - No type in subscription object
- `"Missing user address for orderUpdates"` - orderUpdates requires user field
- `"Missing coin for l2Book"` - l2Book/bbo requires coin field
- `"Unsupported subscription type: <type>"` - Unknown subscription type

## Implementation Details

### Architecture

The WebSocket implementation consists of:

1. **WebSocketManager** (`websocket.go`) - Manages connections and subscriptions
2. **State Integration** - Broadcasts updates when order state changes
3. **Handler Integration** - Exposes `/ws` endpoint

### Broadcasting Behavior

- **Order Updates**: Sent immediately when order state changes (create, modify, fill, cancel)
- **BBO Updates**:
  - Initial snapshot sent immediately after subscription
  - Updates can be triggered manually via `SetBBO()` or `TriggerBBOUpdate()`
  - In production, would update periodically or on price changes

### Thread Safety

- All WebSocket operations are thread-safe
- Concurrent subscriptions and broadcasts are supported
- Each connection has its own write mutex to prevent concurrent writes

### Performance Considerations

- Order update channel: 100 message buffer
- L2 book update channel: 100 message buffer
- Dropped messages logged as warnings when buffers full

## Compliance with Upstream API

This implementation follows the official Hyperliquid WebSocket API specifications from:
- `/upstream-api-docs/websocket.md`
- `/upstream-api-docs/subscriptions.md`

Key differences from production API:
- User tracking simplified (uses placeholder in mock)
- BBO updates are manual/on-demand (vs. real-time market data)
- Limited to orderUpdates and l2Book/bbo subscriptions
- No authentication required

## Future Enhancements

Potential additions (not currently in scope):
- Additional subscription types (trades, candles, userFills, etc.)
- Automatic periodic BBO updates
- User-based order filtering
- WebSocket authentication
- Connection heartbeat/ping-pong
- Reconnection handling
