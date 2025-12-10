package agents

import (
	"context"
	"fmt"
	"github.com/bytedance/sonic"

	"github.com/cloudwego/eino/schema"
	"github.com/oak/crypto-trading-bot/internal/config"
	"github.com/oak/crypto-trading-bot/internal/dataflows"
)

// MarketDataTool provides market data and technical indicators
type MarketDataTool struct {
	marketData *dataflows.MarketData
	config     *config.Config
}

// NewMarketDataTool creates a new market data tool
func NewMarketDataTool(cfg *config.Config) *MarketDataTool {
	return &MarketDataTool{
		marketData: dataflows.NewMarketData(cfg),
		config:     cfg,
	}
}

// Info returns tool information
func (t *MarketDataTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "get_market_data",
		Desc: "Get OHLCV market data and calculate technical indicators (RSI, MACD, Bollinger Bands, SMA, EMA, ATR)",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"symbol": {
				Type:     schema.String,
				Desc:     "Trading pair symbol (e.g., BTCUSDT)",
				Required: true,
			},
			"timeframe": {
				Type:     schema.String,
				Desc:     "Timeframe for candlesticks (1m, 5m, 15m, 1h, 4h, 1d)",
				Required: false,
			},
		}),
	}, nil
}

// InvokableRun executes the tool
func (t *MarketDataTool) InvokableRun(ctx context.Context, argumentsInJSON string) (string, error) {
	var args struct {
		Symbol    string `json:"symbol"`
		Timeframe string `json:"timeframe,omitempty"`
	}

	if err := sonic.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	// Use default timeframe if not provided
	timeframe := args.Timeframe
	if timeframe == "" {
		timeframe = t.config.CryptoTimeframe
	}

	// Fetch OHLCV data
	ohlcvData, err := t.marketData.GetOHLCV(ctx, args.Symbol, timeframe, t.config.CryptoLookbackDays)
	if err != nil {
		return "", fmt.Errorf("failed to fetch market data: %w", err)
	}

	// Calculate indicators
	indicators := dataflows.CalculateIndicators(ohlcvData)

	// Generate report
	report := dataflows.FormatIndicatorReport(args.Symbol, timeframe, ohlcvData, indicators)

	return report, nil
}

// CryptoDataTool provides crypto-specific data (funding rate, order book, etc.)
type CryptoDataTool struct {
	marketData *dataflows.MarketData
	config     *config.Config
}

// NewCryptoDataTool creates a new crypto data tool
func NewCryptoDataTool(cfg *config.Config) *CryptoDataTool {
	return &CryptoDataTool{
		marketData: dataflows.NewMarketData(cfg),
		config:     cfg,
	}
}

// Info returns tool information
func (t *CryptoDataTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "get_crypto_data",
		Desc: "Get crypto-specific data: funding rate, order book depth, 24h statistics",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"symbol": {
				Type:     schema.String,
				Desc:     "Trading pair symbol (e.g., BTCUSDT)",
				Required: true,
			},
			"data_type": {
				Type:     schema.String,
				Desc:     "Type of data: funding_rate, order_book, or stats_24h",
				Required: true,
			},
		}),
	}, nil
}

// InvokableRun executes the tool
func (t *CryptoDataTool) InvokableRun(ctx context.Context, argumentsInJSON string) (string, error) {
	var args struct {
		Symbol   string `json:"symbol"`
		DataType string `json:"data_type"`
	}

	if err := sonic.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	switch args.DataType {
	case "funding_rate":
		rate, err := t.marketData.GetFundingRate(ctx, args.Symbol)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Funding Rate: %.6f (%.4f%%)", rate, rate*100), nil

	case "order_book":
		orderBook, err := t.marketData.GetOrderBook(ctx, args.Symbol, 20)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Order Book - Bid Volume: %.2f, Ask Volume: %.2f, Bid/Ask Ratio: %.2f",
			orderBook["bid_volume"], orderBook["ask_volume"], orderBook["bid_ask_ratio"]), nil

	case "stats_24h":
		stats, err := t.marketData.Get24HrStats(ctx, args.Symbol)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("24h Stats - Price Change: %s%%, High: $%s, Low: $%s, Volume: %s",
			stats["price_change_percent"], stats["high_price"], stats["low_price"], stats["volume"]), nil

	default:
		return "", fmt.Errorf("unknown data type: %s", args.DataType)
	}
}

// SentimentTool provides market sentiment analysis
type SentimentTool struct {
	config *config.Config
}

// NewSentimentTool creates a new sentiment tool
func NewSentimentTool(cfg *config.Config) *SentimentTool {
	return &SentimentTool{
		config: cfg,
	}
}

// Info returns tool information
func (t *SentimentTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "get_sentiment",
		Desc: "Get market sentiment indicators from CryptoOracle (positive/negative ratio, net sentiment)",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"symbol": {
				Type:     schema.String,
				Desc:     "Cryptocurrency symbol (currently only BTC supported)",
				Required: true,
			},
		}),
	}, nil
}

// InvokableRun executes the tool
func (t *SentimentTool) InvokableRun(ctx context.Context, argumentsInJSON string) (string, error) {
	var args struct {
		Symbol string `json:"symbol"`
	}

	if err := sonic.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	sentiment := dataflows.GetSentimentIndicators(ctx, args.Symbol, t.config.BinanceProxy, t.config.BinanceProxyInsecureSkipTLS)
	report := dataflows.FormatSentimentReport(sentiment)

	return report, nil
}
