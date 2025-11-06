//lint:file-ignore ST1005 capitalization due to upstream server we mock
package server

import "fmt"

var (
	ErrOrderTickError   = fmt.Errorf("Price must be divisible by tick size.")
	ErrOrderTick        = fmt.Errorf("Price must be divisible by tick size.")
	ErrOrderMinTradeNtl = fmt.Errorf("Order must have minimum value of $10.")
	// ErrOrderMinTradeSpotNtl should be returned with the addition of the coin token ("%s %s.", ErrOrderMinTradeSpotNtl, token)
	ErrOrderMinTradeSpotNtl = fmt.Errorf("Order must have minimum value of 10.")
	ErrOrderPerpMargin      = fmt.Errorf("Insufficient margin to place order.")
	ErrOrderReduceOnly      = fmt.Errorf("Reduce only order would increase position.")
	// ErrOrderBadAloPx should be returned with the addition of the BBO price ("%s %d.", ErrOrderBadAloPx, bbo)
	ErrOrderBadAloPx                          = fmt.Errorf("Post only order would have immediately matched, bbo was")
	ErrOrderIocCancel                         = fmt.Errorf("Order could not immediately match against any resting orders.")
	ErrOrderBadTriggerPx                      = fmt.Errorf("Invalid TP/SL price.")
	ErrOrderMarketOrderNoLiquidity            = fmt.Errorf("No liquidity available for market order.")
	ErrOrderPositionIncreaseAtOpenInterestCap = fmt.Errorf("Order would increase open interest while open interest is capped")
	ErrOrderPositionFlipAtOpenInterestCap     = fmt.Errorf("Order would increase open interest while open interest is capped")
	ErrOrderTooAggressiveAtOpenInterestCap    = fmt.Errorf("Order rejected due to price more aggressive than oracle while at open interest cap")
	ErrOrderOpenInterestIncrease              = fmt.Errorf("Order would increase open interest too quickly")
	ErrOrderInsufficientSpotBalance           = fmt.Errorf("(Spot-only) Order has insufficient spot balance to trade")
	ErrOrderOracle                            = fmt.Errorf("Order price too far from oracle")
	ErrOrderPerpMaxPosition                   = fmt.Errorf("Order would cause position to exceed margin tier limit at current leverage")
	ErrCancelMissingOrder                     = fmt.Errorf("Order was never placed, already canceled, or filled.")
)
