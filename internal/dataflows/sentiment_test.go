package dataflows

import (
	"context"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// loadEnvConfig loads configuration from .env file
// loadEnvConfig 从 .env 文件加载配置
func loadEnvConfig(t *testing.T) {
	viper.SetConfigType("env")
	viper.SetConfigFile("../../.env") // 相对于 internal/dataflows 的路径
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		t.Logf("⚠️  无法读取 .env 文件: %v (将使用系统环境变量)", err)
	} else {
		t.Logf("✅ 成功加载 .env 文件: %s", viper.ConfigFileUsed())
	}
}

// TestGetSentimentIndicators 测试 CoinMarketCap 恐惧和贪婪指数 API
func TestGetSentimentIndicators(t *testing.T) {
	// 从 .env 文件加载配置
	// Load configuration from .env file
	loadEnvConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	t.Log("开始测试 CoinMarketCap Fear & Greed Index API...")
	t.Logf("API URL: %s", coinMarketCapAPIURL)
	t.Logf("API Key: %s (前10位)", coinMarketCapAPIKey[:10]+"...")

	// 从 .env 文件读取代理配置
	// Read proxy configuration from .env file
	proxyURL := viper.GetString("BINANCE_PROXY")
	insecureSkipTLS := viper.GetBool("BINANCE_PROXY_INSECURE_SKIP_TLS")
	if proxyURL != "" {
		t.Logf("使用代理: %s (TLS Skip: %v)", proxyURL, insecureSkipTLS)
	}

	// 调用 API
	sentiment := GetSentimentIndicators(ctx, "", proxyURL, insecureSkipTLS)

	// 检查结果
	if !sentiment.Success {
		t.Logf("❌ API 调用失败")
		t.Logf("错误信息: %s", sentiment.Error)
		t.Log("")
		t.Log("故障排查提示:")
		t.Log("1. 检查网络连接是否正常")
		t.Log("2. 如果在国内，尝试配置代理或使用 VPN")
		t.Log("3. 验证 API Key 是否有效（访问 https://pro.coinmarketcap.com/account）")
		t.Log("4. 检查防火墙设置是否阻止了 HTTPS 请求")
		t.Fatal("情绪数据获取失败")
	}

	// 成功：打印详细信息
	t.Log("✅ API 调用成功！")
	t.Log("")
	t.Log("========== 市场情绪数据 ==========")
	t.Logf("恐惧和贪婪指数: %d / 100", sentiment.FearGreedValue)
	t.Logf("情绪分类: %s", sentiment.Classification)
	t.Logf("情绪解读: %s", interpretFearGreed(sentiment.FearGreedValue))
	t.Logf("数据时间: %s", sentiment.DataTime)
	t.Logf("数据延迟: %d 分钟", sentiment.DataDelayMinutes)
	t.Log("=================================")
	t.Log("")

	// 生成格式化报告
	report := FormatSentimentReport(sentiment)
	t.Log("格式化报告:")
	t.Logf("%s", report)

	// 验证数据有效性
	if sentiment.FearGreedValue < 0 || sentiment.FearGreedValue > 100 {
		t.Errorf("恐惧和贪婪指数超出范围 (0-100): %d", sentiment.FearGreedValue)
	}

	if sentiment.Classification == "" {
		t.Error("情绪分类为空")
	}

	if sentiment.DataTime == "" {
		t.Error("数据时间为空")
	}

	t.Log("✅ 所有验证通过！")
}

// TestGetSentimentIndicatorsMultipleTimes 测试多次调用 API（检查稳定性）
func TestGetSentimentIndicatorsMultipleTimes(t *testing.T) {
	// 从 .env 文件加载配置
	// Load configuration from .env file
	loadEnvConfig(t)

	t.Log("测试连续调用 API 3 次...")

	// 从 .env 文件读取代理配置
	// Read proxy configuration from .env file
	proxyURL := viper.GetString("BINANCE_PROXY")
	insecureSkipTLS := viper.GetBool("BINANCE_PROXY_INSECURE_SKIP_TLS")

	for i := 1; i <= 3; i++ {
		t.Logf("\n第 %d 次调用:", i)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		sentiment := GetSentimentIndicators(ctx, "", proxyURL, insecureSkipTLS)
		cancel()

		if !sentiment.Success {
			t.Logf("  ❌ 第 %d 次调用失败: %s", i, sentiment.Error)
			continue
		}

		t.Logf("  ✅ 成功 - 指数: %d, 分类: %s", sentiment.FearGreedValue, sentiment.Classification)

		// 两次调用之间等待 1 秒，避免触发速率限制
		if i < 3 {
			time.Sleep(1 * time.Second)
		}
	}
}

// TestInterpretFearGreed 测试恐惧和贪婪指数解读函数
func TestInterpretFearGreed(t *testing.T) {
	testCases := []struct {
		value    int
		expected string
	}{
		{0, "极度恐惧 ❄️ (Extreme Fear)"},
		{10, "极度恐惧 ❄️ (Extreme Fear)"},
		{19, "极度恐惧 ❄️ (Extreme Fear)"},
		{20, "恐惧 📉 (Fear)"},
		{30, "恐惧 📉 (Fear)"},
		{44, "恐惧 📉 (Fear)"},
		{45, "中性 ➖ (Neutral)"},
		{50, "中性 ➖ (Neutral)"},
		{54, "中性 ➖ (Neutral)"},
		{55, "贪婪 📈 (Greed)"},
		{60, "贪婪 📈 (Greed)"},
		{74, "贪婪 📈 (Greed)"},
		{75, "极度贪婪 🔥 (Extreme Greed)"},
		{85, "极度贪婪 🔥 (Extreme Greed)"},
		{100, "极度贪婪 🔥 (Extreme Greed)"},
	}

	for _, tc := range testCases {
		result := interpretFearGreed(tc.value)
		if result != tc.expected {
			t.Errorf("interpretFearGreed(%d) = %s, 期望 %s", tc.value, result, tc.expected)
		}
	}

	t.Log("✅ 所有解读函数测试通过")
}

// TestFormatSentimentReport 测试报告格式化函数
func TestFormatSentimentReport(t *testing.T) {
	// 测试成功情况
	successData := &SentimentData{
		Success:          true,
		FearGreedValue:   21,
		Classification:   "Fear",
		DataTime:         "2025-12-06 01:53:10",
		DataDelayMinutes: 9,
	}

	report := FormatSentimentReport(successData)
	t.Log("成功情况的报告:")
	t.Logf("%s", report)

	if len(report) == 0 {
		t.Error("成功情况下报告不应为空")
	}

	// 测试失败情况
	failData := &SentimentData{
		Success: false,
		Error:   "网络连接失败",
	}

	failReport := FormatSentimentReport(failData)
	t.Log("\n失败情况的报告:")
	t.Logf("%s", failReport)

	if len(failReport) == 0 {
		t.Error("失败情况下报告不应为空")
	}

	t.Log("✅ 报告格式化测试通过")
}

// BenchmarkGetSentimentIndicators 性能基准测试
func BenchmarkGetSentimentIndicators(b *testing.B) {
	// 从 .env 文件加载配置
	// Load configuration from .env file
	viper.SetConfigType("env")
	viper.SetConfigFile("../../.env")
	viper.AutomaticEnv()
	if err := viper.ReadInConfig(); err != nil {
		b.Logf("⚠️  无法读取 .env 文件: %v (将使用系统环境变量)", err)
	}

	ctx := context.Background()

	// 从 .env 文件读取代理配置
	// Read proxy configuration from .env file
	proxyURL := viper.GetString("BINANCE_PROXY")
	insecureSkipTLS := viper.GetBool("BINANCE_PROXY_INSECURE_SKIP_TLS")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GetSentimentIndicators(ctx, "", proxyURL, insecureSkipTLS)
		// 避免触发 API 速率限制
		time.Sleep(100 * time.Millisecond)
	}
}
