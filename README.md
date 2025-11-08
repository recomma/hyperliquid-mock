# Hyperliquid Mock Server

A minimal Go implementation of the Hyperliquid API for E2E testing without requiring the real Hyperliquid service to be online or having USDT available.

## Features

- **Signature validation and wallet isolation** - Recovers wallet addresses from ECDSA signatures for proper multi-wallet testing
- **In-memory order state** - Tracks orders for realistic responses
- **WebSocket support** - Real-time order updates and market data (BBO) with wallet-based filtering
- **Unlimited requests** - No rate limiting
- **Simple responses** - Returns success for all valid operations

## Installation

### As a standalone binary

```bash
git clone https://github.com/recomma/hyperliquid-mock.git
cd hyperliquid-mock
go build -o hyperliquid-mock
```

### As a Go module (for testing)

```bash
go get github.com/recomma/hyperliquid-mock
```

Then import in your tests:

```go
import "github.com/recomma/hyperliquid-mock/server"
```

## Usage

### Start the server

```bash
./hyperliquid-mock -addr :8080
```

Or using `go run`:

```bash
go run main.go -addr :8080
```

### Configuration

The server accepts the following flags:

- `-addr` - Server address (default: `:8080`)

### Configure your application

Point your Recomma application to use the mock server by setting:

```bash
export HYPERLIQUIDURL="http://localhost:8080"
```

Or in your secrets configuration file.

## Upstream API Documentation

The `upstream-api-docs/` directory contains documentation for the real Hyperliquid API. This is provided as a reference for LLMs and developers who want to expand the mock server's capabilities. The mock server currently implements only a subset of the full API.

### Currently Implemented Actions

**Exchange endpoint (/exchange):**
- `order` - Order creation
- `cancel` / `cancelByCloid` - Order cancellation
- `modify` - Single order modification
- `batchModify` - Batch order modifications

**Info endpoint (/info):**
- `orderStatus` - Query order by OID or CLOID
- `metaAndAssetCtxs` - Perpetual futures metadata
- `spotMetaAndAssetCtxs` - Spot trading metadata
- `meta` - Simplified perpetual metadata
- `spotMeta` - Simplified spot metadata

All other action types return a generic success response but do not implement real functionality.

## Endpoints

### WebSocket /ws

Real-time data streaming for order updates and market data. See [WEBSOCKET.md](WEBSOCKET.md) for detailed documentation.

**Supported subscriptions:**
- `orderUpdates` - Real-time order status updates
- `l2Book` / `bbo` - Best bid/offer market data

**Example connection:**
```javascript
const ws = new WebSocket('ws://localhost:8080/ws');

ws.onopen = () => {
  ws.send(JSON.stringify({
    method: 'subscribe',
    subscription: { type: 'orderUpdates', user: '0x1234...' }
  }));
};

ws.onmessage = (event) => {
  const update = JSON.parse(event.data);
  console.log('Order update:', update);
};
```

## Wallet Isolation

The mock server implements proper wallet isolation to support multi-wallet testing scenarios. Each order is tagged with the wallet address recovered from its ECDSA signature, ensuring that:

- **WebSocket subscriptions** only receive updates for orders belonging to the subscribed wallet
- **Order queries** only return orders owned by the requesting wallet
- **Different wallets** can operate independently without interference

### Signature Recovery

The server recovers wallet addresses from ECDSA signatures using the same EIP-712 signing format as the real Hyperliquid API:

1. Msgpack-encodes the action with sorted keys
2. Appends nonce, vault address (optional), and expiration timestamp (optional)
3. Creates an EIP-712 typed data structure with "Agent" phantom type
4. Recovers the public key from the signature
5. Derives the Ethereum address

This allows the mock server to properly isolate orders by wallet without requiring manual configuration.

### Testing Multi-Wallet Scenarios

```go
// Create two different wallets
privateKey1, _ := crypto.GenerateKey()
privateKey2, _ := crypto.GenerateKey()

wallet1 := crypto.PubkeyToAddress(privateKey1.PublicKey).Hex()
wallet2 := crypto.PubkeyToAddress(privateKey2.PublicKey).Hex()

// Create exchanges for each wallet
exchange1 := hyperliquid.NewExchange(ctx, privateKey1, ts.URL(), nil, "", wallet1, nil)
exchange2 := hyperliquid.NewExchange(ctx, privateKey2, ts.URL(), nil, "", wallet2, nil)

// Orders are automatically isolated by wallet
exchange1.Order(ctx, order1, nil)  // Only visible to wallet1
exchange2.Order(ctx, order2, nil)  // Only visible to wallet2
```

See `server/integration_test.go` for complete multi-wallet test examples.

### POST /exchange

Handles trading actions:
- Order creation
- Order modification
- Order cancellation

**Example request (order creation):**
```json
{
  "action": {
    "type": "order",
    "orders": [{
      "coin": "ETH",
      "is_buy": true,
      "sz": 1.5,
      "limit_px": 3000.00,
      "reduce_only": false,
      "order_type": {"limit": {"tif": "Gtc"}},
      "cloid": "00000000000000000000000000000001"
    }]
  },
  "nonce": 1234567890,
  "signature": {
    "r": "0x...",
    "s": "0x...",
    "v": 27
  }
}
```

**Example response:**
```json
{
  "status": "ok",
  "response": {
    "type": "order",
    "data": {
      "statuses": [
        {
          "resting": {
            "oid": 1000001
          }
        }
      ]
    }
  }
}
```

### POST /info

Handles queries:
- `orderStatus` - Query order status by OID or CLOID
- `metaAndAssetCtxs` - Get perpetual futures metadata
- `spotMetaAndAssetCtxs` - Get spot trading metadata

**Example request (order status):**
```json
{
  "type": "orderStatus",
  "oid": 1000001
}
```

**Example response:**
```json
{
  "status": "order",
  "order": {
    "order": {
      "coin": "ETH",
      "side": "B",
      "limitPx": "3000.00",
      "sz": "1.5",
      "oid": 1000001,
      "timestamp": 1234567890000,
      "origSz": "1.5",
      "cloid": "00000000000000000000000000000001"
    },
    "status": "open",
    "statusTimestamp": 1234567890000
  }
}
```

### GET /health

Health check endpoint.

**Example response:**
```json
{
  "status": "healthy",
  "time": "1234567890"
}
```

## Using in Go Tests

The mock server can be embedded directly in your Go tests with automatic request capture and inspection.

### Basic Test Usage

```go
import (
    "context"
    "crypto/ecdsa"
    "testing"

    "github.com/ethereum/go-ethereum/crypto"
    "github.com/recomma/hyperliquid-mock/server"
    "github.com/sonirico/go-hyperliquid"
)

func TestMyHyperliquidCode(t *testing.T) {
    // Create isolated test server (auto-cleanup)
    ts := server.NewTestServer(t)
    ctx := context.Background()

    // Configure Hyperliquid client to use the mock
    privateKey, _ := crypto.HexToECDSA("your-private-key-hex")
    pub := privateKey.Public()
    pubECDSA, _ := pub.(*ecdsa.PublicKey)
    walletAddr := crypto.PubkeyToAddress(*pubECDSA).Hex()

    exchange := hyperliquid.NewExchange(
        ctx,
        privateKey,
        ts.URL(), // Use mock server URL
        nil,      // meta
        "",       // vaultAddr (empty for non-vault)
        walletAddr,
        nil,      // spotMeta
    )

    // Run your test code
    // CLOID must be exactly 32 hex characters (16 bytes)
    token := make([]byte, 16)
    rand.Read(token)
    cloid := hex.EncodeToString(token)
    status, err := exchange.Order(ctx, hyperliquid.CreateOrderRequest{
        Coin:  "ETH",
        IsBuy: true,
        Size:  1.0,
        Price: 3000.0,
        OrderType: hyperliquid.OrderType{
            Limit: &hyperliquid.LimitOrderType{Tif: hyperliquid.TifGtc},
        },
        ClientOrderID: &cloid,
    }, nil)

    // Inspect what was sent to the mock server
    requests := ts.GetExchangeRequests()
    if len(requests) != 1 {
        t.Fatalf("Expected 1 request, got %d", len(requests))
    }

    // Check server state
    order, exists := ts.GetOrder(cloid)
    if !exists {
        t.Fatal("Order not found")
    }
    if order.Status != "open" {
        t.Errorf("Expected status open, got %s", order.Status)
    }
}
```

### Capturing Request/Response Logs

You can provide your own [`slog`](https://pkg.go.dev/log/slog) logger to the test
server to observe every request and response as they flow through the mock. This
is especially useful when running with `DEBUG` level logging:

```go
import (
    "context"
    "crypto/ecdsa"
    "log/slog"
    "os"
    "testing"

    "github.com/ethereum/go-ethereum/crypto"
    "github.com/recomma/hyperliquid-mock/server"
    "github.com/sonirico/go-hyperliquid"
)

func TestMyHyperliquidCode(t *testing.T) {
    logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
    ts := server.NewTestServer(t, server.WithLogger(logger))
    defer ts.Close()

    // ... use ts in your tests ...
}
```

### Test Isolation

Each test gets its own server instance on a random port with isolated request history:

```go
func TestOrderCreation(t *testing.T) {
    ts := server.NewTestServer(t)
    // ts.RequestCount() == 0
    // Make requests...
}

func TestOrderCancellation(t *testing.T) {
    ts := server.NewTestServer(t)
    // Completely separate from TestOrderCreation
    // ts.RequestCount() == 0
}
```

### Request Inspection

Capture and inspect all requests sent to the mock:

```go
// Get typed exchange requests
exchangeReqs := ts.GetExchangeRequests()

// Get typed info requests
infoReqs := ts.GetInfoRequests()

// Get raw requests with headers/body
rawReqs := ts.GetRequests()
for _, req := range rawReqs {
    fmt.Println(req.Method, req.Path)
    fmt.Println(string(req.Body))
    fmt.Println(req.Headers)
}
```

### State Inspection

Check the server's internal order state:

```go
// Get order by CLOID (must be the exact CLOID used when creating the order)
order, exists := ts.GetOrder("00000000000000000000000000000001")
if exists {
    fmt.Println(order.Order.Coin)    // "ETH"
    fmt.Println(order.Status)         // "open"
    fmt.Println(order.Order.Oid)      // 1000001
}

// Get order by OID
order, exists := ts.GetOrderByOid(1000001)
```

### Simulating Fills in Tests

The `FillOrder` helper mutates the in-memory order state that powers `/info`
responses, letting you simulate trade executions directly from your tests.

```go
// Simulate a full fill at 3025.50 (remaining size defaults to zero)
if err := ts.FillOrder(cloid, 3025.50); err != nil {
    t.Fatalf("FillOrder failed: %v", err)
}

// Later, /info orderStatus will report the order as filled with size 0
order, _ := ts.GetOrder(cloid)
fmt.Println(order.Status)     // "filled"
fmt.Println(order.Order.Sz)   // "0"
fmt.Println(order.Order.LimitPx) // "3025.5"

// Simulate a partial fill by specifying the filled quantity
if err := ts.FillOrder(cloid, 2999.25, server.WithFillSize(0.4)); err != nil {
    t.Fatalf("FillOrder failed: %v", err)
}

order, _ = ts.GetOrder(cloid)
fmt.Println(order.Status)    // "open" (still has remaining size)
fmt.Println(order.Order.Sz)  // "0.6" (remaining amount)
```

### Clear Request History

Clear captured requests between test phases:

```go
// Phase 1: Setup
makeOrder(ts, "ETH", 1.0)
assert.Equal(t, 1, ts.RequestCount())

// Clear history
ts.ClearRequests()
assert.Equal(t, 0, ts.RequestCount())

// Phase 2: Test actual behavior
makeOrder(ts, "BTC", 0.5)
assert.Equal(t, 1, ts.RequestCount())
```

### Example: Full Integration Test

See `server/integration_test.go` for complete working examples including:
- Testing with real go-hyperliquid library
- Order creation, modification, cancellation
- Order status queries
- Parallel test safety
- Request inspection patterns

## Manual Testing with curl

You can also test the mock server manually using curl:

```bash
# Health check
curl http://localhost:8080/health

# Create an order (note: CLOID must be 32 hex characters)
curl -X POST http://localhost:8080/exchange \
  -H "Content-Type: application/json" \
  -d '{
    "action": {
      "type": "order",
      "orders": [{
        "coin": "ETH",
        "is_buy": true,
        "sz": 1.0,
        "limit_px": 3000.00,
        "cloid": "00000000000000000000000000000001"
      }]
    },
    "nonce": 123,
    "signature": {"r": "0x", "s": "0x", "v": 27}
  }'

# Query metadata
curl -X POST http://localhost:8080/info \
  -H "Content-Type: application/json" \
  -d '{"type": "metaAndAssetCtxs"}'
```

## Limitations

- **No real order matching** - Orders are just stored in memory, no actual trading logic
- **Limited WebSocket subscriptions** - Only orderUpdates and l2Book/bbo (no trades, candles, etc.)
- **Simplified responses** - Some fields may be omitted or simplified compared to production API
- **No persistence** - All state is lost when the server restarts
- **No rate limiting** - Unlimited requests accepted
- **Testnet behavior** - Signature recovery uses testnet EIP-712 domain (chainId: 1337)

## Development

### Project structure

```
hyperliquid-mock/
├── main.go                    # Binary entry point
├── go.mod                     # Go module file
├── go.sum                     # Go dependencies
├── README.md                  # This file
├── WEBSOCKET.md               # WebSocket API documentation
├── upstream-api-docs/         # Real Hyperliquid API reference (for expansion)
│   ├── exchange-endpoint.md
│   ├── info-endpoint.md
│   ├── perpetuals.md
│   ├── spot.md
│   ├── websocket.md
│   └── subscriptions.md
└── server/
    ├── server.go              # HTTP server setup
    ├── handlers.go            # Request handlers
    ├── handlers_test.go       # Handler unit tests
    ├── signature_recovery.go  # ECDSA signature recovery for wallet isolation
    ├── websocket.go           # WebSocket connection & subscription management
    ├── websocket_example_test.go  # WebSocket usage examples
    ├── types.go               # Request/response types
    ├── state.go               # In-memory state management
    ├── testserver.go          # Test helpers & request capture
    ├── testserver_test.go     # TestServer usage examples
    └── integration_test.go    # Full integration tests with go-hyperliquid
```

### Adding new endpoints

1. Add types to `server/types.go`
2. Implement handler in `server/handlers.go`
3. Register route in `server/server.go`

## License

This is a testing utility for the Recomma project.
