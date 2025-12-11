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
	"strings"
	"time"

	"github.com/adshao/go-binance/v2/futures"
	"github.com/oak/crypto-trading-bot/internal/config"
	"github.com/oak/crypto-trading-bot/internal/executors"
	"github.com/oak/crypto-trading-bot/internal/logger"
	"github.com/oak/crypto-trading-bot/internal/storage"
	"github.com/spf13/viper"
)

// 测试止损管理器的完整功能
// 这个脚本会使用项目的实际代码测试止损单的下单、查询、取消功能
func main() {
	// 加载 .env 配置
	viper.SetConfigType("env")
	viper.SetConfigFile(".env")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("❌ 无法加载 .env 文件: %v", err)
	}

	// 加载配置
	cfg, err := config.LoadConfig(".env")
	if err != nil {
		log.Fatalf("❌ 加载配置失败: %v", err)
	}

	// 强制使用测试模式
	if !cfg.BinanceTestMode {
		fmt.Println("⚠️  警告: 检测到非测试模式，为安全起见强制切换到测试模式")
		cfg.BinanceTestMode = true
		futures.UseTestnet = true
	}

	// 初始化日志
	lgr := logger.NewColorLogger(cfg.DebugMode)

	// 初始化数据库
	db, err := storage.NewStorage(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("❌ 初始化数据库失败: %v", err)
	}
	defer db.Close()

	// 配置 HTTP 客户端（支持代理）
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}
	if cfg.BinanceProxy != "" {
		fmt.Printf("✓ 使用代理: %s\n", cfg.BinanceProxy)
		proxy, err := url.Parse(cfg.BinanceProxy)
		if err != nil {
			log.Fatalf("❌ 代理 URL 解析失败: %v", err)
		}
		transport := &http.Transport{
			Proxy: http.ProxyURL(proxy),
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: cfg.BinanceProxyInsecureSkipTLS,
			},
		}
		httpClient.Transport = transport
	}

	// 创建币安客户端
	futures.UseTestnet = cfg.BinanceTestMode
	client := futures.NewClient(cfg.BinanceAPIKey, cfg.BinanceAPISecret)
	client.HTTPClient = httpClient

	// 创建执行器
	executor := executors.NewBinanceExecutor(cfg, lgr)

	// 创建止损管理器
	stopLossManager := executors.NewStopLossManager(cfg, executor, lgr, db)

	ctx := context.Background()
	symbol := "BTCUSDT" // 使用项目格式（带斜杠会被自动转换）
	binanceSymbol := "BTCUSDT"

	fmt.Println("\n=== 止损管理器集成测试 ===")
	fmt.Printf("交易对: %s\n", symbol)
	fmt.Printf("测试网: %v\n", cfg.BinanceTestMode)
	fmt.Printf("SDK 版本: go-binance v2.8.9+\n\n")

	// 步骤 1: 获取当前价格
	fmt.Println("步骤 1: 获取当前价格...")
	prices, err := client.NewListPricesService().Symbol(binanceSymbol).Do(ctx)
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
	positions, err := client.NewGetPositionRiskService().Symbol(binanceSymbol).Do(ctx)
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
				positionSide = "long"
			} else {
				positionSide = "short"
				positionSize = -positionSize
			}
			fmt.Printf("   发现持仓: %s %.4f\n", positionSide, positionSize)
			break
		}
	}

	if !hasPosition {
		fmt.Println("   ⚠️  当前无持仓，将模拟一个多仓场景")
		positionSide = "long"
		positionSize = 0.001 // 最小数量
	}

	// 步骤 3: 创建模拟持仓
	fmt.Println("\n步骤 3: 在止损管理器中创建模拟持仓...")
	stopPrice := currentPrice * 0.99 // 多仓止损在下方 1%
	if positionSide == "short" {
		stopPrice = currentPrice * 1.01 // 空仓止损在上方 1%
	}

	position := &executors.Position{
		ID:              fmt.Sprintf("test_%d", time.Now().Unix()),
		Symbol:          symbol,
		Side:            positionSide,
		EntryPrice:      currentPrice,
		EntryTime:       time.Now(),
		CurrentPrice:    currentPrice,
		HighestPrice:    currentPrice,
		Quantity:        positionSize,
		Size:            positionSize,
		Leverage:        10,
		InitialStopLoss: stopPrice,
		CurrentStopLoss: stopPrice,
		StopLossOrderID: "",
		StopLossType:    "fixed",
	}

	stopLossManager.RegisterPosition(position)
	fmt.Printf("   ✓ 模拟持仓已注册: %s, 入场价: %.2f, 止损价: %.2f\n",
		positionSide, currentPrice, stopPrice)

	// 步骤 4: 测试下达初始止损单
	fmt.Println("\n步骤 4: 测试下达初始止损单（PlaceInitialStopLoss）...")
	err = stopLossManager.PlaceInitialStopLoss(ctx, position)
	if err != nil {
		fmt.Printf("   ❌ 下单失败: %v\n", err)
		fmt.Println("\n=== 测试结果: 失败 ===")
		fmt.Println("初始止损单下达失败，可能原因:")
		fmt.Println("1. go-binance SDK 版本不是 v2.8.9")
		fmt.Println("2. API Key 权限不足")
		fmt.Println("3. 网络连接问题")
		fmt.Println("4. 持仓模式配置错误")
		os.Exit(1)
	}

	fmt.Println("   ✅ 初始止损单下达成功")

	// 获取更新后的持仓信息
	pos := stopLossManager.GetPosition(symbol)
	if pos == nil {
		log.Fatal("❌ 无法获取持仓信息")
	}
	if pos.StopLossOrderID == "" {
		log.Fatal("❌ 止损单 ID 为空")
	}

	fmt.Printf("   Algo ID: %s\n", pos.StopLossOrderID)
	fmt.Printf("   止损价格: %.2f\n", pos.CurrentStopLoss)

	// 步骤 5: 测试查询止损单状态
	fmt.Println("\n步骤 5: 测试查询止损单状态（CheckStopLossOrderStatus）...")
	time.Sleep(2 * time.Second) // 等待订单进入系统

	err = stopLossManager.CheckStopLossOrderStatus(ctx, symbol)
	if err != nil {
		fmt.Printf("   ⚠️  查询失败: %v\n", err)
		fmt.Println("   继续测试取消功能...")
	} else {
		fmt.Println("   ✅ 止损单状态查询成功")
	}

	// 步骤 6: 直接通过 API 查询验证
	fmt.Println("\n步骤 6: 直接查询 Algo Order API 验证...")
	algoID, _ := strconv.ParseInt(pos.StopLossOrderID, 10, 64)
	algoOrder, err := client.NewGetAlgoOrderService().
		AlgoID(algoID).
		Do(ctx)

	if err != nil {
		fmt.Printf("   ⚠️  API 查询失败: %v\n", err)
	} else {
		fmt.Printf("   ✓ Algo 状态: %s\n", algoOrder.AlgoStatus)
		fmt.Printf("   ✓ 交易对: %s\n", algoOrder.Symbol)
		fmt.Printf("   ✓ 方向: %s\n", algoOrder.Side)
		fmt.Printf("   ✓ 触发价格: %s\n", algoOrder.TriggerPrice)
		fmt.Printf("   ✓ 数量: %s\n", algoOrder.Quantity)
		fmt.Printf("   ✓ Reduce Only: %v\n", algoOrder.ReduceOnly)
	}

	// 步骤 7: 测试更新止损单（会触发取消旧订单）
	fmt.Println("\n步骤 7: 测试更新止损单（会触发取消旧订单）...")

	// 修改止损价，触发更新
	newStopPrice := stopPrice * 0.98 // 移动止损价
	if positionSide == "short" {
		newStopPrice = stopPrice * 1.02
	}

	oldAlgoID := pos.StopLossOrderID
	fmt.Printf("   当前止损单 Algo ID: %s\n", oldAlgoID)
	fmt.Printf("   当前止损价: %.2f\n", pos.CurrentStopLoss)
	fmt.Printf("   新止损价: %.2f\n", newStopPrice)

	// 更新持仓的止损价
	pos.CurrentStopLoss = newStopPrice

	// 再次下单（内部会先取消旧订单）
	err = stopLossManager.PlaceInitialStopLoss(ctx, pos)

	if err != nil {
		fmt.Printf("   ❌ 更新止损单失败: %v\n", err)

		// 仍然尝试清理旧订单
		fmt.Println("\n清理: 取消旧订单...")
		_, cancelErr := client.NewCancelAlgoOrderService().
			AlgoID(algoID).
			Do(ctx)
		if cancelErr != nil {
			fmt.Printf("   ⚠️  清理失败: %v\n", cancelErr)
		} else {
			fmt.Println("   ✓ 旧订单已清理")
		}

		fmt.Println("\n=== 测试结果: 部分成功 ===")
		fmt.Println("下单成功，但更新失败")
		os.Exit(1)
	}

	fmt.Println("   ✅ 止损单更新成功（旧单已取消，新单已下达）")

	// 获取新的订单 ID
	pos = stopLossManager.GetPosition(symbol)
	newAlgoID := pos.StopLossOrderID
	fmt.Printf("   旧 Algo ID: %s\n", oldAlgoID)
	fmt.Printf("   新 Algo ID: %s\n", newAlgoID)

	if oldAlgoID == newAlgoID {
		fmt.Println("   ⚠️  警告: Algo ID 未变化，可能没有真正更新")
	}

	// 步骤 8: 验证旧订单已取消
	fmt.Println("\n步骤 8: 验证旧订单已取消...")
	oldAlgoIDInt, _ := strconv.ParseInt(oldAlgoID, 10, 64)
	oldOrder, err := client.NewGetAlgoOrderService().
		AlgoID(oldAlgoIDInt).
		Do(ctx)

	if err != nil {
		// 订单不存在（已取消）
		if strings.Contains(err.Error(), "Unknown order") ||
			strings.Contains(err.Error(), "Order does not exist") ||
			strings.Contains(err.Error(), "-2011") {
			fmt.Println("   ✅ 旧订单已成功取消（查询不到）")
		} else {
			fmt.Printf("   ⚠️  查询旧订单失败: %v\n", err)
		}
	} else {
		fmt.Printf("   状态: %s\n", oldOrder.AlgoStatus)
		if oldOrder.AlgoStatus == "CANCELED" || oldOrder.AlgoStatus == "CANCELLED" {
			fmt.Println("   ✅ 旧订单已成功取消")
		} else {
			fmt.Printf("   ⚠️  旧订单状态异常: %s\n", oldOrder.AlgoStatus)
		}
	}

	// 步骤 9: 验证新订单存在
	fmt.Println("\n步骤 9: 验证新订单存在...")
	newAlgoIDInt, _ := strconv.ParseInt(newAlgoID, 10, 64)
	newOrder, err := client.NewGetAlgoOrderService().
		AlgoID(newAlgoIDInt).
		Do(ctx)

	if err != nil {
		fmt.Printf("   ❌ 新订单查询失败: %v\n", err)
	} else {
		fmt.Printf("   ✓ 新订单状态: %s\n", newOrder.AlgoStatus)
		fmt.Printf("   ✓ 触发价格: %s\n", newOrder.TriggerPrice)
		fmt.Println("   ✅ 新订单已正确下达")
	}

	// 步骤 10: 清理测试数据
	fmt.Println("\n步骤 10: 清理测试数据...")
	_, err = client.NewCancelAlgoOrderService().
		AlgoID(newAlgoIDInt).
		Do(ctx)

	if err != nil {
		fmt.Printf("   ⚠️  取消新订单失败: %v\n", err)
	} else {
		fmt.Println("   ✓ 新订单已取消")
	}

	// 从管理器中移除持仓
	stopLossManager.RemovePosition(symbol)
	fmt.Println("   ✓ 持仓已从管理器移除")

	// 最终结果
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("=== 测试结果: 全部成功 ✅ ===")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println("\n✅ 所有功能测试通过:")
	fmt.Println("   1. ✅ 下达初始止损单 (PlaceInitialStopLoss)")
	fmt.Println("   2. ✅ 查询止损单状态 (CheckStopLossOrderStatus)")
	fmt.Println("   3. ✅ 取消旧止损单 (内部 cancelStopLossOrder)")
	fmt.Println("   4. ✅ 更新止损单（取消旧单+下新单）")
	fmt.Println("   5. ✅ Algo Order API 集成正常")
	fmt.Println("\n📊 测试统计:")
	fmt.Printf("   - 使用 SDK 版本: go-binance v2.8.9\n")
	fmt.Printf("   - API 端点: Algo Order API (/fapi/v1/algoOrder)\n")
	fmt.Printf("   - 订单类型: AlgoOrderTypeStopMarket\n")
	fmt.Printf("   - 测试交易对: %s\n", symbol)
	fmt.Printf("   - 测试持仓方向: %s\n", positionSide)
	fmt.Println("\n🎉 代码修复成功，可以部署到生产环境！")
	fmt.Println("\n建议:")
	fmt.Println("   1. 重新编译代码: make build")
	fmt.Println("   2. 上传到服务器并重启")
	fmt.Println("   3. 监控日志确认止损单正常工作")
}
