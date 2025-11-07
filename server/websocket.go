package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketManager handles WebSocket connections and subscriptions
type WebSocketManager struct {
	mu             sync.RWMutex
	connections    map[*websocket.Conn]*ConnectionState
	logger         *slog.Logger
	orderUpdatesCh chan OrderUpdate
	l2BookCh       chan L2BookUpdate
	upgrader       websocket.Upgrader
}

// ConnectionState tracks subscriptions for a single WebSocket connection
type ConnectionState struct {
	conn             *websocket.Conn
	mu               sync.RWMutex
	orderUpdatesUser string // empty if not subscribed
	l2BookCoins      map[string]bool
	writeMu          sync.Mutex // Protects concurrent writes to conn
}

// SubscriptionMessage represents a subscription request from the client
type SubscriptionMessage struct {
	Method       string                 `json:"method"`
	Subscription map[string]interface{} `json:"subscription"`
}

// SubscriptionResponse is sent to acknowledge a subscription
type SubscriptionResponse struct {
	Channel string                 `json:"channel"`
	Data    map[string]interface{} `json:"data"`
}

// OrderUpdate represents an order state change to broadcast
// For the mock server, we broadcast to all subscribers
type OrderUpdate struct {
	Orders []WsOrder
}

// WsOrder matches the Hyperliquid WebSocket order format
type WsOrder struct {
	Order           WsBasicOrder `json:"order"`
	Status          string       `json:"status"`
	StatusTimestamp int64        `json:"statusTimestamp"`
}

// WsBasicOrder contains basic order information
type WsBasicOrder struct {
	Coin      string  `json:"coin"`
	Side      string  `json:"side"`
	LimitPx   string  `json:"limitPx"`
	Sz        string  `json:"sz"`
	Oid       int64   `json:"oid"`
	Timestamp int64   `json:"timestamp"`
	OrigSz    string  `json:"origSz"`
	Cloid     *string `json:"cloid,omitempty"`
}

// L2BookUpdate represents a market data update to broadcast
type L2BookUpdate struct {
	Coin   string
	Levels [2][]WsLevel
	Time   int64
}

// WsLevel represents a price level in the order book
type WsLevel struct {
	Px string `json:"px"`
	Sz string `json:"sz"`
	N  int    `json:"n"`
}

// BBOUpdate is an alternative simpler format for BBO updates
type BBOUpdate struct {
	Coin string     `json:"coin"`
	Time int64      `json:"time"`
	BBO  [2]WsLevel `json:"bbo"` // [bid, ask]
}

// NewWebSocketManager creates a new WebSocket manager
func NewWebSocketManager(logger *slog.Logger) *WebSocketManager {
	if logger == nil {
		logger = slog.Default()
	}

	wsm := &WebSocketManager{
		connections:    make(map[*websocket.Conn]*ConnectionState),
		logger:         logger,
		orderUpdatesCh: make(chan OrderUpdate, 100),
		l2BookCh:       make(chan L2BookUpdate, 100),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for testing
			},
		},
	}

	// Start broadcaster goroutines
	go wsm.broadcastOrderUpdates()
	go wsm.broadcastL2Book()

	return wsm
}

// HandleConnection upgrades HTTP to WebSocket and manages the connection
func (wsm *WebSocketManager) HandleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := wsm.upgrader.Upgrade(w, r, nil)
	if err != nil {
		wsm.logger.Error("failed to upgrade connection", "error", err)
		return
	}

	// Register connection
	state := &ConnectionState{
		conn:        conn,
		l2BookCoins: make(map[string]bool),
	}

	wsm.mu.Lock()
	wsm.connections[conn] = state
	wsm.mu.Unlock()

	wsm.logger.Info("websocket connection established", "remote", conn.RemoteAddr())

	// Clean up on disconnect
	defer func() {
		wsm.mu.Lock()
		delete(wsm.connections, conn)
		wsm.mu.Unlock()
		conn.Close()
		wsm.logger.Info("websocket connection closed", "remote", conn.RemoteAddr())
	}()

	// Read messages from client
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				wsm.logger.Error("websocket read error", "error", err)
			}
			break
		}

		wsm.handleMessage(state, message)
	}
}

// handleMessage processes subscription/unsubscription messages
func (wsm *WebSocketManager) handleMessage(state *ConnectionState, message []byte) {
	var subMsg SubscriptionMessage
	if err := json.Unmarshal(message, &subMsg); err != nil {
		wsm.logger.Error("failed to parse subscription message", "error", err, "message", string(message))
		wsm.sendError(state.conn, "Invalid subscription message")
		return
	}

	wsm.logger.Debug("received subscription message", "method", subMsg.Method, "subscription", subMsg.Subscription)

	switch subMsg.Method {
	case "subscribe":
		wsm.handleSubscribe(state, subMsg.Subscription)
	case "unsubscribe":
		wsm.handleUnsubscribe(state, subMsg.Subscription)
	default:
		wsm.sendError(state.conn, "Unknown method: "+subMsg.Method)
	}
}

// handleSubscribe processes a subscription request
func (wsm *WebSocketManager) handleSubscribe(state *ConnectionState, sub map[string]interface{}) {
	subType, ok := sub["type"].(string)
	if !ok {
		wsm.sendError(state.conn, "Missing subscription type")
		return
	}

	switch subType {
	case "orderUpdates":
		user, ok := sub["user"].(string)
		if !ok {
			wsm.sendError(state.conn, "Missing user address for orderUpdates")
			return
		}
		state.mu.Lock()
		state.orderUpdatesUser = user
		state.mu.Unlock()

		wsm.logger.Info("subscribed to orderUpdates", "user", user)

		// Send subscription acknowledgment
		wsm.sendSubscriptionResponse(state.conn, sub)

	case "l2Book":
		coin, ok := sub["coin"].(string)
		if !ok {
			wsm.sendError(state.conn, "Missing coin for l2Book")
			return
		}
		state.mu.Lock()
		state.l2BookCoins[coin] = true
		state.mu.Unlock()

		wsm.logger.Info("subscribed to l2Book", "coin", coin)

		// Send subscription acknowledgment
		wsm.sendSubscriptionResponse(state.conn, sub)

		// Send initial BBO snapshot
		wsm.sendInitialBBO(state.conn, coin)

	case "bbo":
		coin, ok := sub["coin"].(string)
		if !ok {
			wsm.sendError(state.conn, "Missing coin for bbo")
			return
		}
		state.mu.Lock()
		state.l2BookCoins[coin] = true
		state.mu.Unlock()

		wsm.logger.Info("subscribed to bbo", "coin", coin)

		// Send subscription acknowledgment
		wsm.sendSubscriptionResponse(state.conn, sub)

		// Send initial BBO snapshot
		wsm.sendInitialBBO(state.conn, coin)

	default:
		wsm.sendError(state.conn, "Unsupported subscription type: "+subType)
	}
}

// handleUnsubscribe processes an unsubscription request
func (wsm *WebSocketManager) handleUnsubscribe(state *ConnectionState, sub map[string]interface{}) {
	subType, ok := sub["type"].(string)
	if !ok {
		return
	}

	switch subType {
	case "orderUpdates":
		state.mu.Lock()
		state.orderUpdatesUser = ""
		state.mu.Unlock()

	case "l2Book", "bbo":
		coin, ok := sub["coin"].(string)
		if !ok {
			return
		}
		state.mu.Lock()
		delete(state.l2BookCoins, coin)
		state.mu.Unlock()
	}
}

// sendSubscriptionResponse sends a subscription acknowledgment
func (wsm *WebSocketManager) sendSubscriptionResponse(conn *websocket.Conn, subscription map[string]interface{}) {
	response := SubscriptionResponse{
		Channel: "subscriptionResponse",
		Data: map[string]interface{}{
			"method":       "subscribe",
			"subscription": subscription,
		},
	}

	wsm.sendJSON(conn, response)
}

// sendError sends an error message to the client
func (wsm *WebSocketManager) sendError(conn *websocket.Conn, errorMsg string) {
	msg := map[string]string{
		"channel": "error",
		"error":   errorMsg,
	}
	wsm.sendJSON(conn, msg)
}

// sendJSON sends a JSON message to a connection
func (wsm *WebSocketManager) sendJSON(conn *websocket.Conn, v interface{}) {
	// Find the connection state to use its write mutex
	wsm.mu.RLock()
	state, ok := wsm.connections[conn]
	wsm.mu.RUnlock()

	if !ok {
		return
	}

	state.writeMu.Lock()
	defer state.writeMu.Unlock()

	if err := conn.WriteJSON(v); err != nil {
		wsm.logger.Error("failed to send JSON", "error", err)
	}
}

// BroadcastOrderUpdate queues an order update for broadcasting
// In the mock server, we broadcast to all orderUpdates subscribers
func (wsm *WebSocketManager) BroadcastOrderUpdate(order *OrderDetail) {
	if order == nil {
		return
	}

	wsOrder := WsOrder{
		Order: WsBasicOrder{
			Coin:      order.Order.Coin,
			Side:      order.Order.Side,
			LimitPx:   order.Order.LimitPx,
			Sz:        order.Order.Sz,
			Oid:       order.Order.Oid,
			Timestamp: order.Order.Timestamp,
			OrigSz:    order.Order.OrigSz,
			Cloid:     order.Order.Cloid,
		},
		Status:          order.Status,
		StatusTimestamp: order.StatusTimestamp,
	}

	update := OrderUpdate{
		Orders: []WsOrder{wsOrder},
	}

	select {
	case wsm.orderUpdatesCh <- update:
	default:
		wsm.logger.Warn("order updates channel full, dropping update")
	}
}

// broadcastOrderUpdates broadcasts order updates to subscribed clients
// In the mock server, we broadcast to all orderUpdates subscribers
func (wsm *WebSocketManager) broadcastOrderUpdates() {
	for update := range wsm.orderUpdatesCh {
		wsm.mu.RLock()
		for conn, state := range wsm.connections {
			state.mu.RLock()
			// In the mock, broadcast to anyone subscribed to orderUpdates
			if state.orderUpdatesUser != "" {
				state.mu.RUnlock()
				msg := map[string]interface{}{
					"channel": "orderUpdates",
					"data":    update.Orders,
				}
				wsm.sendJSON(conn, msg)
			} else {
				state.mu.RUnlock()
			}
		}
		wsm.mu.RUnlock()
	}
}

// BroadcastL2Book queues an L2 book update for broadcasting
func (wsm *WebSocketManager) BroadcastL2Book(coin string, levels [2][]WsLevel) {
	update := L2BookUpdate{
		Coin:   coin,
		Levels: levels,
		Time:   time.Now().UnixMilli(),
	}

	select {
	case wsm.l2BookCh <- update:
	default:
		wsm.logger.Warn("l2book channel full, dropping update")
	}
}

// broadcastL2Book broadcasts L2 book updates to subscribed clients
func (wsm *WebSocketManager) broadcastL2Book() {
	for update := range wsm.l2BookCh {
		wsm.mu.RLock()
		for conn, state := range wsm.connections {
			state.mu.RLock()
			if state.l2BookCoins[update.Coin] {
				state.mu.RUnlock()
				msg := map[string]interface{}{
					"channel": "l2Book",
					"data": map[string]interface{}{
						"coin":   update.Coin,
						"time":   update.Time,
						"levels": update.Levels,
					},
				}
				wsm.sendJSON(conn, msg)
			} else {
				state.mu.RUnlock()
			}
		}
		wsm.mu.RUnlock()
	}
}

// sendInitialBBO sends an initial BBO snapshot to a newly subscribed client
func (wsm *WebSocketManager) sendInitialBBO(conn *websocket.Conn, coin string) {
	// Get mock BBO prices based on coin
	bid, ask := wsm.getMockBBO(coin)

	msg := map[string]interface{}{
		"channel": "l2Book",
		"data": map[string]interface{}{
			"coin": coin,
			"time": time.Now().UnixMilli(),
			"levels": [2][]WsLevel{
				{{Px: bid.Px, Sz: bid.Sz, N: 1}},
				{{Px: ask.Px, Sz: ask.Sz, N: 1}},
			},
		},
	}

	wsm.sendJSON(conn, msg)
}

// getMockBBO returns mock bid/ask prices for a coin
func (wsm *WebSocketManager) getMockBBO(coin string) (bid WsLevel, ask WsLevel) {
	switch coin {
	case "BTC":
		// Use 87000 as the reference price (from IOC tests)
		return WsLevel{Px: "86956.5", Sz: "10.5", N: 1},
			WsLevel{Px: "87043.5", Sz: "8.3", N: 1}
	case "ETH":
		return WsLevel{Px: "2999.5", Sz: "50.0", N: 1},
			WsLevel{Px: "3000.5", Sz: "45.0", N: 1}
	case "SOL":
		return WsLevel{Px: "99.9", Sz: "100.0", N: 1},
			WsLevel{Px: "100.1", Sz: "95.0", N: 1}
	default:
		// Default: use a spread of 0.1%
		return WsLevel{Px: "999.5", Sz: "10.0", N: 1},
			WsLevel{Px: "1000.5", Sz: "10.0", N: 1}
	}
}

// SetBBO allows tests to manually set BBO prices
func (wsm *WebSocketManager) SetBBO(coin string, bidPx, bidSz, askPx, askSz string) {
	levels := [2][]WsLevel{
		{{Px: bidPx, Sz: bidSz, N: 1}},
		{{Px: askPx, Sz: askSz, N: 1}},
	}
	wsm.BroadcastL2Book(coin, levels)
}

// TriggerBBOUpdate forces an immediate BBO update for a coin
func (wsm *WebSocketManager) TriggerBBOUpdate(coin string) {
	bid, ask := wsm.getMockBBO(coin)
	levels := [2][]WsLevel{
		{bid},
		{ask},
	}
	wsm.BroadcastL2Book(coin, levels)
}
