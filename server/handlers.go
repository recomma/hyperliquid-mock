package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Handler manages HTTP requests for the mock server
type Handler struct {
	state  *State
	logger *slog.Logger
}

// NewHandler creates a new request handler
func NewHandler(opts ...Option) *Handler {
	cfg := options{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	logger := cfg.logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Handler{
		state:  NewState(),
		logger: logger,
	}
}

// HandleExchange handles POST /exchange requests
func (h *Handler) HandleExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("failed to decode exchange request", "error", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	h.logger.Debug("exchange request received", "request", req)

	// Parse the action to determine the operation type
	actionMap, ok := req.Action.(map[string]interface{})
	if !ok {
		http.Error(w, "Invalid action format", http.StatusBadRequest)
		return
	}

	// Determine action type
	var response ExchangeResponse
	if order, ok := actionMap["type"].(string); ok {
		switch order {
		case "order":
			response = h.handleOrder(actionMap)
		case "cancel", "cancelByCloid":
			response = h.handleCancel(actionMap)
		case "modify":
			response = h.handleModify(actionMap)
		case "batchModify":
			response = h.handleBatchModify(actionMap)
		default:
			response = ExchangeResponse{Status: "ok", Response: &ExchangeActionData{Type: "default"}}
		}
	} else {
		// Try to detect action type from the structure
		if _, hasOrders := actionMap["orders"]; hasOrders {
			response = h.handleOrder(actionMap)
		} else if _, hasCancels := actionMap["cancels"]; hasCancels {
			response = h.handleCancel(actionMap)
		} else if _, hasModifies := actionMap["modifies"]; hasModifies {
			response = h.handleBatchModify(actionMap)
		} else {
			response = ExchangeResponse{Status: "ok", Response: &ExchangeActionData{Type: "default"}}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	h.logger.Debug("exchange response", "response", response)

	json.NewEncoder(w).Encode(response)
}

// handleOrder processes order creation/modification
func (h *Handler) handleOrder(action map[string]interface{}) ExchangeResponse {
	// Extract order details
	var statuses []OrderStatusResponse

	// Check if it's a single order or batch
	if orders, ok := action["orders"].([]interface{}); ok {
		for _, o := range orders {
			orderMap, _ := o.(map[string]interface{})
			status := h.processOrder(orderMap)
			statuses = append(statuses, status)
		}
	} else {
		// Single order
		status := h.processOrder(action)
		statuses = append(statuses, status)
	}

	return ExchangeResponse{
		Status: "ok",
		Response: &ExchangeActionData{
			Type: "order",
			Data: map[string]interface{}{
				"statuses": statuses,
			},
		},
	}
}

// processOrder creates or modifies a single order
func (h *Handler) processOrder(orderMap map[string]interface{}) OrderStatusResponse {
	// Handle both full field names and abbreviated format
	// Abbreviated: a=asset, b=is_buy, c=cloid, o=oid, p=price, s=size
	var coin string
	var isBuy bool
	var sz, limitPx float64
	var cloid string
	var oid int64
	var hasOid bool

	// Try abbreviated format first (used by go-hyperliquid)
	if assetIdx, ok := orderMap["a"].(float64); ok {
		// Map asset index to coin name
		coin = h.mapAssetIndexToCoin(int(assetIdx))
		isBuy, _ = orderMap["b"].(bool)
		sz = parseNumeric(orderMap["s"])
		limitPx = parseNumeric(orderMap["p"])
		cloid, _ = orderMap["c"].(string)

		// Check for oid (for modify operations)
		if oidVal, ok := orderMap["o"].(float64); ok {
			oid = int64(oidVal)
			hasOid = true
		} else if oidVal, ok := orderMap["o"].(string); ok {
			// Parse hex string
			if len(oidVal) > 2 && oidVal[:2] == "0x" {
				oidVal = oidVal[2:]
			}
			parsed, err := strconv.ParseInt(oidVal, 16, 64)
			if err == nil {
				oid = parsed
				hasOid = true
			}
		}
	} else {
		// Fall back to full field names
		coin, _ = orderMap["coin"].(string)
		isBuy, _ = orderMap["is_buy"].(bool)
		sz = parseNumeric(orderMap["sz"])
		limitPx = parseNumeric(orderMap["limit_px"])
		cloid, _ = orderMap["cloid"].(string)

		// Check for oid
		if oidVal, ok := orderMap["oid"].(int64); ok {
			oid = oidVal
			hasOid = true
		} else if oidVal, ok := orderMap["oid"].(float64); ok {
			oid = int64(oidVal)
			hasOid = true
		}
	}

	side := "B"
	if !isBuy {
		side = "A"
	}

	szStr := fmt.Sprintf("%.8g", sz)
	pxStr := fmt.Sprintf("%.8g", limitPx)

	// Check if this is a modification (has oid)
	if hasOid {
		if modifiedOid, ok := h.state.ModifyOrderByOid(oid, pxStr, szStr); ok {
			// Get cloid for the response if it exists
			if order, exists := h.state.GetOrderByOid(modifiedOid); exists && order.Order.Cloid != nil {
				return OrderStatusResponse{
					Resting: &RestingStatus{Oid: modifiedOid, Cloid: order.Order.Cloid},
				}
			}
			return OrderStatusResponse{
				Resting: &RestingStatus{Oid: modifiedOid},
			}
		}
		// If we have an OID but the order wasn't found, return an error
		errMsg := fmt.Sprintf("Order not found: oid=%d", oid)
		return OrderStatusResponse{
			Error: &errMsg,
		}
	}

	// Check if this is a modification by cloid
	if cloid != "" {
		if modifiedOid, ok := h.state.ModifyOrder(cloid, pxStr, szStr); ok {
			return OrderStatusResponse{
				Resting: &RestingStatus{Oid: modifiedOid, Cloid: &cloid},
			}
		}
	}

	if tif := extractTimeInForce(orderMap); strings.EqualFold(tif, "ioc") {
		errMsg := ErrOrderIocCancel.Error()
		return OrderStatusResponse{Error: &errMsg}
	}

	// Create new order
	if cloid == "" {
		cloid = fmt.Sprintf("mock-%d", time.Now().UnixNano())
	}

	newOid := h.state.CreateOrder(cloid, coin, side, pxStr, szStr)

	return OrderStatusResponse{
		Resting: &RestingStatus{Oid: newOid, Cloid: &cloid},
	}
}

func extractTimeInForce(orderMap map[string]interface{}) string {
	if tRaw, ok := orderMap["t"]; ok {
		if tMap, ok := tRaw.(map[string]interface{}); ok {
			if limitRaw, ok := tMap["limit"]; ok {
				if limitMap, ok := limitRaw.(map[string]interface{}); ok {
					if tif, ok := limitMap["tif"].(string); ok {
						return tif
					}
				}
			}
		}
	}

	if tif, ok := orderMap["tif"].(string); ok {
		return tif
	}

	if tif, ok := orderMap["timeInForce"].(string); ok {
		return tif
	}

	return ""
}

// mapAssetIndexToCoin maps an asset index to a coin name
func (h *Handler) mapAssetIndexToCoin(index int) string {
	// Standard mapping based on common perpetual futures order
	assets := []string{"BTC", "ETH", "SOL", "ARB"}
	if index >= 0 && index < len(assets) {
		return assets[index]
	}
	return fmt.Sprintf("ASSET_%d", index)
}

// handleModify processes a single order modification
func (h *Handler) handleModify(action map[string]interface{}) ExchangeResponse {
	// Extract oid (can be numeric or hex string)
	var oid int64
	var hasOid bool

	if oidVal, ok := action["oid"].(float64); ok {
		oid = int64(oidVal)
		hasOid = true
	} else if oidVal, ok := action["oid"].(string); ok {
		// Parse hex string or cloid
		if len(oidVal) > 2 && oidVal[:2] == "0x" {
			// Try to parse as hex OID
			parsed, err := strconv.ParseInt(oidVal[2:], 16, 64)
			if err == nil {
				oid = parsed
				hasOid = true
			} else {
				// It's a cloid, not an OID
				// Try to find the order by cloid
				if order, exists := h.state.GetOrder(oidVal); exists {
					oid = order.Order.Oid
					hasOid = true
				}
			}
		}
	}

	if !hasOid {
		errMsg := "Missing or invalid oid"
		return ExchangeResponse{
			Status: "ok",
			Response: &ExchangeActionData{
				Type: "order",
				Data: map[string]interface{}{
					"statuses": []OrderStatusResponse{{Error: &errMsg}},
				},
			},
		}
	}

	// Extract the order object
	orderMap, ok := action["order"].(map[string]interface{})
	if !ok {
		errMsg := "Missing order object"
		return ExchangeResponse{
			Status: "ok",
			Response: &ExchangeActionData{
				Type: "order",
				Data: map[string]interface{}{
					"statuses": []OrderStatusResponse{{Error: &errMsg}},
				},
			},
		}
	}

	// Inject the oid into the order map so processOrder can handle it
	orderMap["o"] = float64(oid)

	// Process the modification
	status := h.processOrder(orderMap)

	return ExchangeResponse{
		Status: "ok",
		Response: &ExchangeActionData{
			Type: "order",
			Data: map[string]interface{}{
				"statuses": []OrderStatusResponse{status},
			},
		},
	}
}

// parseNumeric extracts a float64 from a variety of input types commonly used
// in Hyperliquid payloads (numbers encoded as floats, strings, or json.Number).
func parseNumeric(value interface{}) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case string:
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			return parsed
		}
	case json.Number:
		if parsed, err := v.Float64(); err == nil {
			return parsed
		}
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return 0
}

// handleCancel processes order cancellation
func (h *Handler) handleCancel(action map[string]interface{}) ExchangeResponse {
	var statuses []interface{}

	if cancels, ok := action["cancels"].([]interface{}); ok {
		for _, c := range cancels {
			cancelMap, _ := c.(map[string]interface{})

			// Try to cancel by OID first (abbreviated: o)
			if oidVal, ok := cancelMap["o"].(float64); ok {
				oid := int64(oidVal)
				if h.state.CancelOrderByOid(oid) {
					statuses = append(statuses, "success")
				} else {
					statuses = append(statuses, map[string]interface{}{
						"error": fmt.Sprintf("Order was never placed, already canceled, or filled. oid=%d", oid),
					})
				}
				continue
			}

			// Try by cloid (abbreviated format: c)
			cloid, ok := cancelMap["c"].(string)
			if !ok {
				// Fall back to full field names
				cloid, _ = cancelMap["cloid"].(string)
			}

			if cloid != "" {
				if h.state.CancelOrder(cloid) {
					statuses = append(statuses, "success")
				} else {
					statuses = append(statuses, map[string]interface{}{
						"error": fmt.Sprintf("Order was never placed, already canceled, or filled. cloid=%s", cloid),
					})
				}
			}
		}
	} else {
		// Single cancel - try both OID and cloid
		if oidVal, ok := action["o"].(float64); ok {
			oid := int64(oidVal)
			if h.state.CancelOrderByOid(oid) {
				statuses = append(statuses, "success")
			}
		} else {
			cloid, ok := action["c"].(string)
			if !ok {
				cloid, _ = action["cloid"].(string)
			}
			if cloid != "" {
				if h.state.CancelOrder(cloid) {
					statuses = append(statuses, "success")
				}
			}
		}
	}

	return ExchangeResponse{
		Status: "ok",
		Response: &ExchangeActionData{
			Type: "cancel",
			Data: map[string]interface{}{
				"statuses": statuses,
			},
		},
	}
}

// handleBatchModify processes batch order modifications
func (h *Handler) handleBatchModify(action map[string]interface{}) ExchangeResponse {
	var statuses []OrderStatusResponse

	if modifies, ok := action["modifies"].([]interface{}); ok {
		for _, m := range modifies {
			modifyMap, _ := m.(map[string]interface{})
			orderMap, _ := modifyMap["order"].(map[string]interface{})
			cloid, _ := modifyMap["cloid"].(string)

			if cloid != "" && orderMap != nil {
				sz, _ := orderMap["sz"].(float64)
				limitPx, _ := orderMap["limit_px"].(float64)

				szStr := fmt.Sprintf("%.8g", sz)
				pxStr := fmt.Sprintf("%.8g", limitPx)

				if oid, ok := h.state.ModifyOrder(cloid, pxStr, szStr); ok {
					statuses = append(statuses, OrderStatusResponse{
						Resting: &RestingStatus{Oid: oid, Cloid: &cloid},
					})
				}
			}
		}
	}

	return ExchangeResponse{
		Status: "ok",
		Response: &ExchangeActionData{
			Type: "default",
			Data: map[string]interface{}{
				"statuses": statuses,
			},
		},
	}
}

// HandleInfo handles POST /info requests
func (h *Handler) HandleInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req InfoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("failed to decode info request", "error", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	var response interface{}

	switch req.Type {
	case "orderStatus":
		response = h.handleOrderStatus(req)
	case "metaAndAssetCtxs":
		response = h.handleMetaAndAssetCtxs()
	case "spotMetaAndAssetCtxs":
		response = h.handleSpotMetaAndAssetCtxs()
	case "meta":
		response = h.handleMeta()
	case "spotMeta":
		response = h.handleSpotMeta()
	default:
		http.Error(w, "Unknown info type", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	json.NewEncoder(w).Encode(response)
}

// handleOrderStatus queries order status by cloid or oid
func (h *Handler) handleOrderStatus(req InfoRequest) OrderQueryResult {
	var order *OrderDetail
	var exists bool

	if req.Oid != nil && !req.Oid.Valid() && req.Cloid == nil {
		if raw := req.Oid.Raw(); raw != "" {
			req.Cloid = &raw
		}
	}

	// Try to query by OID first
	if req.Oid != nil && req.Oid.Valid() {
		order, exists = h.state.GetOrderByOid(req.Oid.Int64())
	} else if req.Cloid != nil && *req.Cloid != "" {
		// Query by CLOID
		order, exists = h.state.GetOrder(*req.Cloid)
	} else if req.User != "" {
		// In a real implementation, we'd filter by user
		// For the mock, we just return unknown
		return OrderQueryResult{Status: "unknown_cloid"}
	}

	if !exists || order == nil {
		return OrderQueryResult{Status: "unknown_cloid"}
	}

	return OrderQueryResult{
		Status: "order",
		Order:  order,
	}
}

// handleMetaAndAssetCtxs returns mock perpetual futures metadata
func (h *Handler) handleMetaAndAssetCtxs() MetaAndAssetCtxs {
	return MetaAndAssetCtxs{
		Universe: []MetaUniverse{
			{Name: "BTC", SzDecimals: 5},
			{Name: "ETH", SzDecimals: 4},
			{Name: "SOL", SzDecimals: 1},
			{Name: "ARB", SzDecimals: 0},
		},
		MarginTables: [][]interface{}{
			{
				1,
				map[string]interface{}{
					"description": "Standard",
					"marginTiers": []map[string]interface{}{
						{"lowerBound": "0.0", "maxLeverage": 50},
						{"lowerBound": "100000.0", "maxLeverage": 25},
						{"lowerBound": "500000.0", "maxLeverage": 10},
					},
				},
			},
			{
				2,
				map[string]interface{}{
					"description": "Alt Coins",
					"marginTiers": []map[string]interface{}{
						{"lowerBound": "0.0", "maxLeverage": 20},
						{"lowerBound": "50000.0", "maxLeverage": 10},
						{"lowerBound": "200000.0", "maxLeverage": 5},
					},
				},
			},
		},
		AssetCtxs: []AssetCtx{
			{
				Funding:      "0.0001",
				OpenInterest: "1000000",
				PrevDayPx:    "50000",
				DayNtlVlm:    "100000000",
				Premium:      "0.0005",
				OraclePx:     "50123.45",
				MarkPx:       "50125.00",
				MidPx:        "50124.00",
			},
			{
				Funding:      "0.00015",
				OpenInterest: "500000",
				PrevDayPx:    "3000",
				DayNtlVlm:    "50000000",
				Premium:      "0.0003",
				OraclePx:     "3012.34",
				MarkPx:       "3013.00",
				MidPx:        "3012.50",
			},
			{
				Funding:      "0.0002",
				OpenInterest: "100000",
				PrevDayPx:    "100",
				DayNtlVlm:    "10000000",
				Premium:      "0.0002",
				OraclePx:     "101.23",
				MarkPx:       "101.25",
				MidPx:        "101.24",
			},
			{
				Funding:      "0.0001",
				OpenInterest: "50000",
				PrevDayPx:    "1.5",
				DayNtlVlm:    "5000000",
				Premium:      "0.0001",
				OraclePx:     "1.51",
				MarkPx:       "1.52",
				MidPx:        "1.515",
			},
		},
	}
}

// handleSpotMetaAndAssetCtxs returns mock spot trading metadata
func (h *Handler) handleSpotMetaAndAssetCtxs() SpotMetaAndAssetCtxs {
	return SpotMetaAndAssetCtxs{
		Tokens: []SpotToken{
			{Name: "USDC", SzDecimals: 6, WeiDecimals: 6, Index: 0, TokenId: "0x1", IsCanonical: true},
			{Name: "BTC", SzDecimals: 8, WeiDecimals: 8, Index: 1, TokenId: "0x2", IsCanonical: true},
			{Name: "ETH", SzDecimals: 18, WeiDecimals: 18, Index: 2, TokenId: "0x3", IsCanonical: true},
		},
		Universe: []SpotUniverse{
			{Tokens: []int{1, 0}, Name: "BTC/USDC", Index: 0},
			{Tokens: []int{2, 0}, Name: "ETH/USDC", Index: 1},
		},
	}
}

// handleMeta returns mock perpetual futures metadata (simpler format than metaAndAssetCtxs)
func (h *Handler) handleMeta() Meta {
	return Meta{
		Universe: []AssetInfo{
			{Name: "BTC", SzDecimals: 5, MaxLeverage: 50, MarginTableId: 1},
			{Name: "ETH", SzDecimals: 4, MaxLeverage: 50, MarginTableId: 1},
			{Name: "SOL", SzDecimals: 1, MaxLeverage: 20, MarginTableId: 2},
			{Name: "ARB", SzDecimals: 0, MaxLeverage: 20, MarginTableId: 2},
		},
		// MarginTables is an array of tuples: [[id, {description, marginTiers}], ...]
		MarginTables: [][]interface{}{
			{1, MarginTable{
				Description: "Standard",
				MarginTiers: []MarginTier{
					{LowerBound: "0.0", MaxLeverage: 50},
					{LowerBound: "100000.0", MaxLeverage: 25},
					{LowerBound: "500000.0", MaxLeverage: 10},
				},
			}},
			{2, MarginTable{
				Description: "Alt Coins",
				MarginTiers: []MarginTier{
					{LowerBound: "0.0", MaxLeverage: 20},
					{LowerBound: "50000.0", MaxLeverage: 10},
					{LowerBound: "200000.0", MaxLeverage: 5},
				},
			}},
		},
	}
}

// handleSpotMeta returns mock spot trading metadata (same structure as spotMetaAndAssetCtxs)
func (h *Handler) handleSpotMeta() SpotMeta {
	return SpotMeta{
		Tokens: []SpotToken{
			{Name: "USDC", SzDecimals: 6, WeiDecimals: 6, Index: 0, TokenId: "0x1", IsCanonical: true},
			{Name: "BTC", SzDecimals: 8, WeiDecimals: 8, Index: 1, TokenId: "0x2", IsCanonical: true},
			{Name: "ETH", SzDecimals: 18, WeiDecimals: 18, Index: 2, TokenId: "0x3", IsCanonical: true},
		},
		Universe: []SpotUniverse{
			{Tokens: []int{1, 0}, Name: "BTC/USDC", Index: 0},
			{Tokens: []int{2, 0}, Name: "ETH/USDC", Index: 1},
		},
	}
}

// HandleHealth handles GET /health for health checks
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
		"time":   strconv.FormatInt(time.Now().Unix(), 10),
	})
}
