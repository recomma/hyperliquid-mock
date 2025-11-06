package server

import (
	"encoding/json"
	"strconv"
)

// ExchangeRequest represents a request to the /exchange endpoint
type ExchangeRequest struct {
	Action    interface{} `json:"action"`
	Nonce     int64       `json:"nonce"`
	Signature struct {
		R string `json:"r"`
		S string `json:"s"`
		V int    `json:"v"`
	} `json:"signature"`
}

// ExchangeResponse is the response from /exchange endpoint
type ExchangeResponse struct {
	Status   string              `json:"status"`
	Response *ExchangeActionData `json:"response,omitempty"`
}

// ExchangeActionData contains the response data for exchange actions
type ExchangeActionData struct {
	Type string                 `json:"type"`
	Data map[string]interface{} `json:"data,omitempty"`
}

// InfoRequest represents a request to the /info endpoint
type InfoRequest struct {
	Type  string       `json:"type"`
	User  string       `json:"user,omitempty"`
	Oid   *FlexibleOid `json:"oid,omitempty"`
	Cloid *string      `json:"cloid,omitempty"`
}

// FlexibleOid is a custom type that can unmarshal from string (hex) or int64
type FlexibleOid int64

// UnmarshalJSON implements custom unmarshaling for FlexibleOid
func (f *FlexibleOid) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as int64 first
	var i int64
	if err := json.Unmarshal(data, &i); err == nil {
		*f = FlexibleOid(i)
		return nil
	}

	// Try to unmarshal as string (hex format)
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	// Parse hex string (with or without 0x prefix)
	if len(s) > 2 && s[:2] == "0x" {
		s = s[2:]
	}

	// Parse as hex
	parsed, err := strconv.ParseInt(s, 16, 64)
	if err != nil {
		return err
	}

	*f = FlexibleOid(parsed)
	return nil
}

// Int64 returns the int64 value
func (f *FlexibleOid) Int64() int64 {
	return int64(*f)
}

// OrderQueryResult is the response for orderStatus queries
type OrderQueryResult struct {
	Status string       `json:"status"`
	Order  *OrderDetail `json:"order,omitempty"`
}

// OrderDetail contains detailed order information
type OrderDetail struct {
	Order           OrderInfo `json:"order"`
	Status          string    `json:"status"`
	StatusTimestamp int64     `json:"statusTimestamp"`
}

// OrderInfo contains basic order information
type OrderInfo struct {
	Coin      string  `json:"coin"`
	Side      string  `json:"side"`
	LimitPx   string  `json:"limitPx"`
	Sz        string  `json:"sz"`
	Oid       int64   `json:"oid"`
	Timestamp int64   `json:"timestamp"`
	OrigSz    string  `json:"origSz"`
	Cloid     *string `json:"cloid,omitempty"`
}

// MetaUniverse represents a trading pair in the metadata
type MetaUniverse struct {
	Name       string `json:"name"`
	SzDecimals int    `json:"szDecimals"`
}

// AssetCtx represents asset context information
type AssetCtx struct {
	Funding       string   `json:"funding"`
	OpenInterest  string   `json:"openInterest"`
	PrevDayPx     string   `json:"prevDayPx"`
	DayNtlVlm     string   `json:"dayNtlVlm"`
	Premium       string   `json:"premium"`
	OraclePx      string   `json:"oraclePx"`
	MarkPx        string   `json:"markPx"`
	MidPx         string   `json:"midPx,omitempty"`
	ImpactPxs     []string `json:"impactPxs,omitempty"`
}

// MetaAndAssetCtxs is the response for metadata queries
// The real API returns this as an array: [meta, assetCtxs]
type MetaAndAssetCtxs struct {
	Universe  []MetaUniverse `json:"universe"`
	AssetCtxs []AssetCtx     `json:"assetCtxs"`
}

// MarshalJSON implements custom JSON marshaling for MetaAndAssetCtxs
// The real Hyperliquid API returns this as a 2-element array, not an object
func (m MetaAndAssetCtxs) MarshalJSON() ([]byte, error) {
	// Return as array: [meta, assetCtxs]
	meta := struct {
		Universe []MetaUniverse `json:"universe"`
	}{
		Universe: m.Universe,
	}

	return json.Marshal([]interface{}{meta, m.AssetCtxs})
}

// SpotToken represents a spot trading token
type SpotToken struct {
	Name         string `json:"name"`
	SzDecimals   int    `json:"szDecimals"`
	WeiDecimals  int    `json:"weiDecimals"`
	Index        int    `json:"index"`
	TokenId      string `json:"tokenId"`
	IsCanonical  bool   `json:"isCanonical"`
}

// SpotUniverse represents a spot trading pair
type SpotUniverse struct {
	Tokens []int  `json:"tokens"`
	Name   string `json:"name"`
	Index  int    `json:"index"`
}

// SpotMetaAndAssetCtxs is the response for spot metadata queries
type SpotMetaAndAssetCtxs struct {
	Tokens   []SpotToken    `json:"tokens"`
	Universe []SpotUniverse `json:"universe"`
}

// Meta is the response for the "meta" info type (simpler than metaAndAssetCtxs)
// MarginTables is an array of tuples: [[id, {description, marginTiers}], ...]
type Meta struct {
	Universe     []AssetInfo     `json:"universe"`
	MarginTables [][]interface{} `json:"marginTables,omitempty"`
}

// AssetInfo contains basic asset information for the meta endpoint
type AssetInfo struct {
	Name          string `json:"name"`
	SzDecimals    int    `json:"szDecimals"`
	MaxLeverage   int    `json:"maxLeverage,omitempty"`
	MarginTableId int    `json:"marginTableId,omitempty"`
	OnlyIsolated  bool   `json:"onlyIsolated,omitempty"`
	IsDelisted    bool   `json:"isDelisted,omitempty"`
}

// MarginTable defines leverage tiers
type MarginTable struct {
	ID          int          `json:"id,omitempty"`
	Description string       `json:"description,omitempty"`
	MarginTiers []MarginTier `json:"marginTiers,omitempty"`
}

// MarginTier defines a margin tier with leverage limits
type MarginTier struct {
	LowerBound  string `json:"lowerBound"`
	MaxLeverage int    `json:"maxLeverage"`
}

// SpotMeta is the response for the "spotMeta" info type
type SpotMeta struct {
	Tokens   []SpotToken    `json:"tokens"`
	Universe []SpotUniverse `json:"universe"`
}

// OrderStatusResponse represents an order status in exchange responses
type OrderStatusResponse struct {
	Resting *RestingStatus `json:"resting,omitempty"`
	Filled  *FilledStatus  `json:"filled,omitempty"`
	Error   *string        `json:"error,omitempty"`
}

// RestingStatus indicates an order is resting on the book
type RestingStatus struct {
	Oid   int64   `json:"oid"`
	Cloid *string `json:"cloid,omitempty"`
}

// FilledStatus indicates an order was filled
type FilledStatus struct {
	TotalSz string `json:"totalSz"`
	AvgPx   string `json:"avgPx,omitempty"`
	Oid     int64  `json:"oid"`
}
