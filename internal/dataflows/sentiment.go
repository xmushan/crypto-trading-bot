package dataflows

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	// CoinMarketCap Fear and Greed API
	// CoinMarketCap 恐惧和贪婪指数 API
	coinMarketCapAPIURL = "https://pro-api.coinmarketcap.com/v3/fear-and-greed/latest"
	coinMarketCapAPIKey = "b62aad6b3a5f456e8e7a3e3aa4048a89"
)

// SentimentData holds market sentiment information
// SentimentData 保存市场情绪信息
type SentimentData struct {
	Success          bool
	FearGreedValue   int    // 0-100, 恐惧和贪婪指数 / Fear and Greed Index
	Classification   string // "Extreme Fear", "Fear", "Neutral", "Greed", "Extreme Greed"
	DataTime         string // 数据更新时间 / Data update time
	DataDelayMinutes int    // 数据延迟分钟数 / Data delay in minutes
	Error            string
}

// CoinMarketCapResponse represents the CMC API response structure
// CoinMarketCapResponse 表示 CMC API 响应结构
type CoinMarketCapResponse struct {
	Data struct {
		Value               int    `json:"value"`
		UpdateTime          string `json:"update_time"`
		ValueClassification string `json:"value_classification"`
	} `json:"data"`
	Status struct {
		Timestamp    string `json:"timestamp"`
		ErrorCode    string `json:"error_code"`
		ErrorMessage string `json:"error_message"`
		Elapsed      int    `json:"elapsed"`
		CreditCount  int    `json:"credit_count"`
	} `json:"status"`
}

// GetSentimentIndicators fetches market sentiment indicators from CoinMarketCap
// GetSentimentIndicators 从 CoinMarketCap 获取市场情绪指标
func GetSentimentIndicators(ctx context.Context, symbol string, proxyURL string, insecureSkipTLS bool) *SentimentData {
	// Note: CMC Fear & Greed Index is market-wide, not symbol-specific
	// 注意：CMC 恐惧和贪婪指数是市场级别的，不针对特定交易对
	_ = symbol // unused but kept for API compatibility

	req, err := http.NewRequestWithContext(ctx, "GET", coinMarketCapAPIURL, nil)
	if err != nil {
		return &SentimentData{
			Success: false,
			Error:   fmt.Sprintf("创建请求失败: %v", err),
		}
	}

	req.Header.Set("X-CMC_PRO_API_KEY", coinMarketCapAPIKey)
	req.Header.Set("Accept", "application/json")

	// Create HTTP client with proxy support
	// 创建支持代理的 HTTP 客户端
	client := createHTTPClientWithProxy(proxyURL, insecureSkipTLS)
	resp, err := client.Do(req)
	if err != nil {
		// Enhanced error message with network troubleshooting hints
		// 增强错误信息，提供网络故障排查提示
		return &SentimentData{
			Success: false,
			Error: fmt.Sprintf("网络请求失败: %v (提示: 如果你在国内，可能需要配置代理或使用 VPN。"+
				"CoinMarketCap API 需要稳定的国际网络连接)", err),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Try to read error body for more details
		// 尝试读取错误响应体以获取更多详情
		body, _ := io.ReadAll(resp.Body)
		return &SentimentData{
			Success: false,
			Error: fmt.Sprintf("HTTP 状态码错误 %d, 响应: %s (提示: 可能是 API Key 无效或请求受限)",
				resp.StatusCode, string(body)),
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &SentimentData{
			Success: false,
			Error:   fmt.Sprintf("读取响应失败: %v", err),
		}
	}

	var apiResp CoinMarketCapResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return &SentimentData{
			Success: false,
			Error: fmt.Sprintf("解析 JSON 失败: %v, 原始响应: %s",
				err, string(body)[:200]), // 只显示前200字符
		}
	}

	if apiResp.Status.ErrorCode != "0" {
		return &SentimentData{
			Success: false,
			Error: fmt.Sprintf("API 错误: code=%s, msg=%s",
				apiResp.Status.ErrorCode, apiResp.Status.ErrorMessage),
		}
	}

	// Parse update time
	// 解析更新时间
	updateTime, err := time.Parse(time.RFC3339, apiResp.Data.UpdateTime)
	if err != nil {
		updateTime = time.Now() // fallback to current time
	}

	dataDelay := int(time.Since(updateTime).Minutes())

	return &SentimentData{
		Success:          true,
		FearGreedValue:   apiResp.Data.Value,
		Classification:   apiResp.Data.ValueClassification,
		DataTime:         updateTime.Format("2006-01-02 15:04:05"),
		DataDelayMinutes: dataDelay,
	}
}

// createHTTPClientWithProxy creates an HTTP client with proxy support
// createHTTPClientWithProxy 创建支持代理的 HTTP 客户端
func createHTTPClientWithProxy(proxyURL string, insecureSkipTLS bool) *http.Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: insecureSkipTLS,
		},
	}

	// Configure proxy if provided
	// 如果提供了代理，则配置代理
	if proxyURL != "" {
		proxy, err := url.Parse(proxyURL)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxy)
		}
	}

	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}
}

// interpretFearGreed interprets the fear and greed index value (0-100)
// interpretFearGreed 解释恐惧和贪婪指数值 (0-100)
func interpretFearGreed(value int) string {
	switch {
	case value >= 75:
		return "极度贪婪 🔥 (Extreme Greed)"
	case value >= 55:
		return "贪婪 📈 (Greed)"
	case value >= 45:
		return "中性 ➖ (Neutral)"
	case value >= 20:
		return "恐惧 📉 (Fear)"
	default:
		return "极度恐惧 ❄️ (Extreme Fear)"
	}
}

// FormatSentimentReport formats sentiment data as a readable report
// FormatSentimentReport 格式化情绪数据为可读报告
func FormatSentimentReport(sentiment *SentimentData) string {
	if !sentiment.Success {
		return fmt.Sprintf("市场情绪数据获取失败: %s", sentiment.Error)
	}

	return fmt.Sprintf(`由 CoinMarketCap 获取的加密货币恐惧与贪婪指数，显示当前市场情绪为: %s (%d/100) `,
		interpretFearGreed(sentiment.FearGreedValue),
		sentiment.FearGreedValue)
}
