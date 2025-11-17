package server

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// ExchangeRequest represents a request to the /exchange endpoint
type ExchangeRequest struct {
	Action       interface{} `json:"action"`
	Nonce        int64       `json:"nonce"`
	VaultAddress *string     `json:"vaultAddress,omitempty"`
	ExpiresAfter *int64      `json:"expiresAfter,omitempty"`
	Signature    struct {
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
	Type string       `json:"type"`
	User string       `json:"user,omitempty"`
	Oid  *FlexibleOid `json:"oid,omitempty"`
}

// FlexibleOid captures order identifiers that can be provided either as raw
// integers or as hex strings. The Valid flag tracks whether parsing succeeded.
type FlexibleOid struct {
	value int64
	valid bool
	raw   string
}

// UnmarshalJSON implements custom unmarshaling for FlexibleOid and gracefully
// handles strings that do not represent an OID (e.g., CLOIDs provided in the
// oid field by some clients).
func (f *FlexibleOid) UnmarshalJSON(data []byte) error {
	// Try numeric forms first (int64 or json.Number)
	var i int64
	if err := json.Unmarshal(data, &i); err == nil {
		f.value = i
		f.valid = true
		f.raw = ""
		return nil
	}

	// Try to unmarshal as string
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	f.raw = s

	// Remove optional 0x prefix before parsing
	normalized := s
	if len(normalized) > 2 && (normalized[:2] == "0x" || normalized[:2] == "0X") {
		normalized = normalized[2:]
	}

	// Attempt hexadecimal parsing
	if parsed, err := strconv.ParseInt(normalized, 16, 64); err == nil {
		f.value = parsed
		f.valid = true
		f.raw = s
		return nil
	}

	// Attempt base-10 parsing for decimal strings
	if parsed, err := strconv.ParseInt(normalized, 10, 64); err == nil {
		f.value = parsed
		f.valid = true
		f.raw = s
		return nil
	}

	// Treat other strings as non-OID values; keep the struct invalid so the
	// caller can fall back to CLOID-based lookups without failing decode.
	f.value = 0
	f.valid = false
	return nil
}

// Valid reports whether the OID was successfully parsed.
func (f *FlexibleOid) Valid() bool {
	return f != nil && f.valid
}

// Int64 returns the parsed OID value. Callers should check Valid() first.
func (f *FlexibleOid) Int64() int64 {
	if f == nil {
		return 0
	}
	return f.value
}

// Raw returns the original string value when provided.
func (f *FlexibleOid) Raw() string {
	if f == nil {
		return ""
	}
	return f.raw
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
	User      string  `json:"user,omitempty"` // Wallet address that owns this order
}

// MetaUniverse represents a trading pair in the metadata
type MetaUniverse struct {
	Name       string `json:"name"`
	SzDecimals int    `json:"szDecimals"`
}

// AssetCtx represents asset context information
type AssetCtx struct {
	Funding      string   `json:"funding"`
	OpenInterest string   `json:"openInterest"`
	PrevDayPx    string   `json:"prevDayPx"`
	DayNtlVlm    string   `json:"dayNtlVlm"`
	Premium      string   `json:"premium"`
	OraclePx     string   `json:"oraclePx"`
	MarkPx       string   `json:"markPx"`
	MidPx        string   `json:"midPx,omitempty"`
	ImpactPxs    []string `json:"impactPxs,omitempty"`
}

// MetaAndAssetCtxs is the response for metadata queries
// The real API returns this as an array: [meta, assetCtxs]
type MetaAndAssetCtxs struct {
	Universe     []MetaUniverse  `json:"universe"`
	AssetCtxs    []AssetCtx      `json:"assetCtxs"`
	MarginTables [][]interface{} `json:"marginTables,omitempty"`
}

// MarshalJSON implements custom JSON marshaling for MetaAndAssetCtxs
// The real Hyperliquid API returns this as a 2-element array, not an object
func (m MetaAndAssetCtxs) MarshalJSON() ([]byte, error) {
	// Return as array: [meta, assetCtxs]
	meta := struct {
		Universe     []MetaUniverse  `json:"universe"`
		MarginTables [][]interface{} `json:"marginTables,omitempty"`
	}{
		Universe:     m.Universe,
		MarginTables: m.MarginTables,
	}

	return json.Marshal([]interface{}{meta, m.AssetCtxs})
}

// UnmarshalJSON implements custom unmarshaling for MetaAndAssetCtxs to accept
// the array response returned by the real API.
func (m *MetaAndAssetCtxs) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if len(raw) != 2 {
		return fmt.Errorf("expected 2 elements in metaAndAssetCtxs response, got %d", len(raw))
	}

	var metaPart struct {
		Universe     []MetaUniverse  `json:"universe"`
		MarginTables [][]interface{} `json:"marginTables,omitempty"`
	}
	if err := json.Unmarshal(raw[0], &metaPart); err != nil {
		return err
	}

	var assetCtxs []AssetCtx
	if err := json.Unmarshal(raw[1], &assetCtxs); err != nil {
		return err
	}

	m.Universe = metaPart.Universe
	m.MarginTables = metaPart.MarginTables
	m.AssetCtxs = assetCtxs
	return nil
}

// SpotToken represents a spot trading token
type SpotToken struct {
	Name                    string                `json:"name"`
	SzDecimals              int                   `json:"szDecimals"`
	WeiDecimals             int                   `json:"weiDecimals"`
	Index                   int                   `json:"index"`
	TokenId                 string                `json:"tokenId"`
	IsCanonical             bool                  `json:"isCanonical"`
	EvmContract             *SpotTokenEvmContract `json:"evmContract,omitempty"`
	FullName                *string               `json:"fullName,omitempty"`
	DeployerTradingFeeShare string                `json:"deployerTradingFeeShare,omitempty"`
}

// SpotTokenEvmContract holds on-chain contract metadata provided by the real API.
type SpotTokenEvmContract struct {
	Address             string `json:"address"`
	EvmExtraWeiDecimals *int   `json:"evm_extra_wei_decimals,omitempty"`
}

// SpotUniverse represents a spot trading pair
type SpotUniverse struct {
	Tokens      []int  `json:"tokens"`
	Name        string `json:"name"`
	Index       int    `json:"index"`
	IsCanonical bool   `json:"isCanonical"`
}

// SpotMetaAndAssetCtxs is the response for spot metadata queries
type SpotMetaAndAssetCtxs struct {
	Tokens    []SpotToken    `json:"tokens"`
	Universe  []SpotUniverse `json:"universe"`
	AssetCtxs []SpotAssetCtx `json:"assetCtxs"`
}

// SpotAssetCtx represents asset context data for spot tokens
type SpotAssetCtx struct {
	PrevDayPx         string `json:"prevDayPx"`
	DayNtlVlm         string `json:"dayNtlVlm"`
	MarkPx            string `json:"markPx"`
	MidPx             string `json:"midPx"`
	CirculatingSupply string `json:"circulatingSupply"`
	Coin              string `json:"coin"`
	TotalSupply       string `json:"totalSupply"`
	DayBaseVlm        string `json:"dayBaseVlm"`
}

// MarshalJSON implements custom JSON marshaling for SpotMetaAndAssetCtxs to
// match the array response returned by the real API: [meta, assetCtxs].
func (m SpotMetaAndAssetCtxs) MarshalJSON() ([]byte, error) {
	meta := struct {
		Tokens   []SpotToken    `json:"tokens"`
		Universe []SpotUniverse `json:"universe"`
	}{Tokens: m.Tokens, Universe: m.Universe}

	return json.Marshal([]interface{}{meta, m.AssetCtxs})
}

// UnmarshalJSON accepts both the array representation returned by the real API
// and a map representation for flexibility in tests.
func (m *SpotMetaAndAssetCtxs) UnmarshalJSON(data []byte) error {
	var arrayForm []json.RawMessage
	if err := json.Unmarshal(data, &arrayForm); err == nil {
		if len(arrayForm) != 2 {
			return fmt.Errorf("expected 2 elements in spotMetaAndAssetCtxs response, got %d", len(arrayForm))
		}

		var meta struct {
			Tokens   []SpotToken    `json:"tokens"`
			Universe []SpotUniverse `json:"universe"`
		}
		if err := json.Unmarshal(arrayForm[0], &meta); err != nil {
			return err
		}

		var assetCtxs []SpotAssetCtx
		if err := json.Unmarshal(arrayForm[1], &assetCtxs); err != nil {
			return err
		}

		m.Tokens = meta.Tokens
		m.Universe = meta.Universe
		m.AssetCtxs = assetCtxs
		return nil
	}

	// Fallback: try object form
	var objectForm struct {
		Tokens    []SpotToken    `json:"tokens"`
		Universe  []SpotUniverse `json:"universe"`
		AssetCtxs []SpotAssetCtx `json:"assetCtxs"`
	}
	if err := json.Unmarshal(data, &objectForm); err != nil {
		return err
	}

	m.Tokens = objectForm.Tokens
	m.Universe = objectForm.Universe
	m.AssetCtxs = objectForm.AssetCtxs
	return nil
}

// Meta is the response for the "meta" info type (simpler than metaAndAssetCtxs)
// MarginTables is an array of tuples: [[id, {description, marginTiers}], ...]
type Meta struct {
	Universe     []AssetInfo     `json:"universe"`
	MarginTables [][]interface{} `json:"marginTables,omitempty"`
}

// UnmarshalJSON normalizes the nested margin tables array so that nested
// slices use map representations, matching the responses from the real API and
// simplifying downstream type assertions in tests.
func (m *Meta) UnmarshalJSON(data []byte) error {
	type metaAlias struct {
		Universe     []AssetInfo       `json:"universe"`
		MarginTables []json.RawMessage `json:"marginTables"`
	}

	var tmp metaAlias
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	m.Universe = tmp.Universe
	if len(tmp.MarginTables) == 0 {
		m.MarginTables = nil
		return nil
	}

	m.MarginTables = make([][]interface{}, 0, len(tmp.MarginTables))
	for _, entry := range tmp.MarginTables {
		var tuple []json.RawMessage
		if err := json.Unmarshal(entry, &tuple); err != nil {
			return err
		}
		if len(tuple) != 2 {
			continue
		}

		var id interface{}
		if err := json.Unmarshal(tuple[0], &id); err != nil {
			return err
		}

		tableMap := make(map[string]interface{})
		if err := json.Unmarshal(tuple[1], &tableMap); err != nil {
			return err
		}

		if tiersRaw, ok := tableMap["marginTiers"]; ok {
			switch tiers := tiersRaw.(type) {
			case []interface{}:
				converted := make([]map[string]interface{}, 0, len(tiers))
				for _, tier := range tiers {
					if tierMap, ok := tier.(map[string]interface{}); ok {
						converted = append(converted, tierMap)
					}
				}
				tableMap["marginTiers"] = converted
			}
		}

		m.MarginTables = append(m.MarginTables, []interface{}{id, tableMap})
	}

	return nil
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
