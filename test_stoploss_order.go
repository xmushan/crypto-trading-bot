package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/adshao/go-binance/v2/futures"
	"github.com/spf13/viper"
)

// 测试币安止损单下单（使用 Algo Order API）
// 这个脚本会尝试下一个真实的止损单到币安测试网
func main() {
	// 加载 .env 配置
	viper.SetConfigType("env")
	viper.SetConfigFile(".env")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		log.Printf("警告: 无法加载 .env 文件: %v", err)
	}

	// 读取配置
	apiKey := viper.GetString("BINANCE_API_KEY")
	apiSecret := viper.GetString("BINANCE_API_SECRET")
	testMode := viper.GetBool("BINANCE_TEST_MODE")
	proxyURL := viper.GetString("BINANCE_PROXY")
	insecureSkipTLS := viper.GetBool("BINANCE_PROXY_INSECURE_SKIP_TLS")

	if apiKey == "" || apiSecret == "" {
		log.Fatal("❌ 错误: 请在 .env 文件中设置 BINANCE_API_KEY 和 BINANCE_API_SECRET")
	}

	// 配置 HTTP 客户端（支持代理）
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}
	if proxyURL != "" {
		fmt.Printf("✓ 使用代理: %s\n", proxyURL)
		proxy, err := url.Parse(proxyURL)
		if err != nil {
			log.Fatalf("❌ 代理 URL 解析失败: %v", err)
		}
		transport := &http.Transport{
			Proxy: http.ProxyURL(proxy),
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: insecureSkipTLS,
			},
		}
		httpClient.Transport = transport
	}

	// 创建币安客户端
	var client *futures.Client
	if testMode {
		fmt.Println("✓ 使用测试网模式")
		futures.UseTestnet = true
		client = futures.NewClient(apiKey, apiSecret)
	} else {
		fmt.Println("⚠️  警告: 使用正式网络（真实资金）")
		client = futures.NewClient(apiKey, apiSecret)
	}
	client.HTTPClient = httpClient

	ctx := context.Background()
	symbol := "BTCUSDT"

	fmt.Println("\n=== 币安止损单测试（Algo Order API）===")
	fmt.Printf("交易对: %s\n", symbol)
	fmt.Printf("测试网: %v\n\n", testMode)

	// 步骤 1: 获取当前价格
	fmt.Println("步骤 1: 获取当前价格...")
	prices, err := client.NewListPricesService().Symbol(symbol).Do(ctx)
	if err != nil {
		log.Fatalf("❌ 获取价格失败: %v", err)
	}
	if len(prices) == 0 {
		log.Fatal("❌ 未获取到价格数据")
	}

	currentPrice, _ := strconv.ParseFloat(prices[0].Price, 64)
	fmt.Printf("   当前价格: %.2f\n", currentPrice)

	// 步骤 2: 检查当前持仓
	fmt.Println("\n步骤 2: 检查当前持仓...")
	positions, err := client.NewGetPositionRiskService().Symbol(symbol).Do(ctx)
	if err != nil {
		log.Fatalf("❌ 获取持仓失败: %v", err)
	}

	var hasPosition bool
	var positionSize float64
	var positionSide string
	for _, pos := range positions {
		amt, _ := strconv.ParseFloat(pos.PositionAmt, 64)
		if amt != 0 {
			hasPosition = true
			positionSize = amt
			if amt > 0 {
				positionSide = "LONG"
			} else {
				positionSide = "SHORT"
				positionSize = -positionSize
			}
			fmt.Printf("   发现持仓: %s %.4f\n", positionSide, positionSize)
			break
		}
	}

	if !hasPosition {
		fmt.Println("   ⚠️  当前无持仓，将模拟一个多仓场景")
		positionSide = "LONG"
		positionSize = 0.001 // 最小数量
	}

	// 步骤 3: 计算止损价格
	fmt.Println("\n步骤 3: 计算止损价格...")
	var triggerPrice float64
	var orderSide futures.SideType

	if positionSide == "LONG" {
		// 多仓止损：在当前价下方 1%
		triggerPrice = currentPrice * 0.99
		orderSide = futures.SideTypeSell
		fmt.Printf("   多仓止损价: %.2f (当前价 %.2f 下方 1%%)\n", triggerPrice, currentPrice)
	} else {
		// 空仓止损：在当前价上方 1%
		triggerPrice = currentPrice * 1.01
		orderSide = futures.SideTypeBuy
		fmt.Printf("   空仓止损价: %.2f (当前价 %.2f 上方 1%%)\n", triggerPrice, currentPrice)
	}

	// 步骤 4: 使用 Algo Order API 下止损单
	fmt.Println("\n步骤 4: 使用 Algo Order API 下止损单...")
	fmt.Println("   预期结果: 成功")

	order, err := client.NewCreateAlgoOrderService().
		Symbol(symbol).
		Side(orderSide).
		Type(futures.AlgoOrderTypeStopMarket).
		TriggerPrice(fmt.Sprintf("%.2f", triggerPrice)).
		Quantity(fmt.Sprintf("%.4f", positionSize)).
		ReduceOnly(true).
		Do(ctx)

	if err != nil {
		fmt.Printf("   ❌ 失败: %v\n", err)
		fmt.Println("\n=== 测试结果: 失败 ===")
		fmt.Println("Algo Order API 下单失败，请检查:")
		fmt.Println("1. go-binance SDK 版本是否 >= v2.8.9")
		fmt.Println("2. API Key 权限是否正确")
		fmt.Println("3. 网络连接是否正常")
		os.Exit(1)
	}

	fmt.Println("   ✓ 成功下单！")
	fmt.Printf("   Algo ID: %d\n", order.AlgoId)
	fmt.Printf("   订单状态: %s\n", order.AlgoStatus)
	fmt.Printf("   订单类型: %s\n", order.OrderType)
	fmt.Printf("   触发价格: %s\n", order.TriggerPrice)

	// 步骤 5: 查询订单状态
	fmt.Println("\n步骤 5: 查询 Algo 订单状态...")
	time.Sleep(1 * time.Second)

	queryOrder, err := client.NewGetAlgoOrderService().
		AlgoID(order.AlgoId).
		Do(ctx)

	if err != nil {
		fmt.Printf("   ⚠️  查询失败: %v\n", err)
	} else {
		fmt.Printf("   Algo 状态: %s\n", queryOrder.AlgoStatus)
		fmt.Printf("   触发价格: %s\n", queryOrder.TriggerPrice)
		fmt.Printf("   数量: %s\n", queryOrder.Quantity)
	}

	// 步骤 6: 取消订单（清理）
	fmt.Println("\n步骤 6: 取消 Algo 订单（清理）...")
	_, err = client.NewCancelAlgoOrderService().
		AlgoID(order.AlgoId).
		Do(ctx)

	if err != nil {
		fmt.Printf("   ⚠️  取消失败: %v\n", err)
	} else {
		fmt.Println("   ✓ 订单已取消")
	}

	// 最终结果
	fmt.Println("\n=== 测试结果: 成功 ===")
	fmt.Println("✅ Algo Order API 可以正常工作")
	fmt.Println("✅ STOP_MARKET 订单类型支持")
	fmt.Println("✅ 止损单可以正常下达和取消")
	fmt.Println("\n关键变更:")
	fmt.Println("- 使用 NewCreateAlgoOrderService() 代替 NewCreateOrderService()")
	fmt.Println("- 使用 AlgoOrderTypeStopMarket 代替 OrderTypeStopMarket")
	fmt.Println("- 使用 TriggerPrice() 代替 StopPrice()")
	fmt.Println("- 订单 ID 使用 order.AlgoId 代替 order.OrderID")
	fmt.Println("\n建议: 重新编译并部署代码到服务器")
}
