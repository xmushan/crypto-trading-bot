# 止损单修复总结

## 问题描述

**错误代码**: `-4120`
**错误消息**: `Order type not supported for this endpoint`

在开仓后下达初始止损单时失败,原因是币安 API 进行了重大变更:所有条件单（STOP_MARKET、STOP、TAKE_PROFIT 等）必须使用新的 **Algo Order API** 端点。

## 根本原因

币安期货 API 变更:
- **旧端点**: `POST /fapi/v1/order` - 不再支持条件单
- **新端点**: `POST /fapi/v1/algoOrder` - 所有条件单必须使用此端点

## 修复内容

### 1. 升级 go-binance SDK

```bash
go get github.com/adshao/go-binance/v2@v2.8.9
```

**原因**: Algo Order API 支持在 v2.8.9 版本中添加（PR #784）

### 2. 修改止损单下单逻辑

**文件**: `internal/executors/stoploss_manager.go`

**修改位置**: `placeStopLossOrder()` 函数（第 944-976 行）

**修改前**:
```go
order, err := sm.executor.client.NewCreateOrderService().
    Symbol(binanceSymbol).
    Side(orderSide).
    Type(futures.OrderTypeStopMarket).  // ❌ 旧的订单类型
    StopPrice(fmt.Sprintf("%.2f", stopPrice)).
    Quantity(fmt.Sprintf("%.4f", pos.Quantity)).
    ReduceOnly(true).
    Do(ctx)

pos.StopLossOrderID = fmt.Sprintf("%d", order.OrderID)
```

**修改后**:
```go
order, err := sm.executor.client.NewCreateAlgoOrderService().  // ✅ 使用 Algo Order 服务
    Symbol(binanceSymbol).
    Side(orderSide).
    Type(futures.AlgoOrderTypeStopMarket).  // ✅ Algo 订单类型
    TriggerPrice(fmt.Sprintf("%.2f", stopPrice)).  // ✅ TriggerPrice 代替 StopPrice
    Quantity(fmt.Sprintf("%.4f", pos.Quantity)).
    ReduceOnly(true).
    Do(ctx)

pos.StopLossOrderID = fmt.Sprintf("%d", order.AlgoId)  // ✅ 使用 AlgoId
```

### 3. 修改止损单取消逻辑

**文件**: `internal/executors/stoploss_manager.go`

**修改位置**: `cancelStopLossOrder()` 函数（第 1005-1007 行）

**修改前**:
```go
_, err := sm.executor.client.NewCancelOrderService().
    Symbol(binanceSymbol).
    OrderID(parseInt64(pos.StopLossOrderID)).
    Do(ctx)
```

**修改后**:
```go
_, err := sm.executor.client.NewCancelAlgoOrderService().  // ✅ 使用 Algo Order 取消服务
    AlgoID(parseInt64(pos.StopLossOrderID)).  // ✅ 只需要 AlgoID，不需要 Symbol
    Do(ctx)
```

### 4. 修改止损单状态查询逻辑

**文件**: `internal/executors/stoploss_manager.go`

**修改位置**: `CheckStopLossOrderStatus()` 函数（第 813-874 行）

**修改前**:
```go
order, err := sm.executor.client.NewGetOrderService().
    Symbol(binanceSymbol).
    OrderID(parseInt64(pos.StopLossOrderID)).
    Do(ctx)

if order.Status == futures.OrderStatusTypeFilled {
    closePrice, _ := parseFloat(order.AvgPrice)
    // ...
}
```

**修改后**:
```go
algoOrder, err := sm.executor.client.NewGetAlgoOrderService().  // ✅ 使用 Algo Order 查询服务
    AlgoID(parseInt64(pos.StopLossOrderID)).  // ✅ 只需要 AlgoID
    Do(ctx)

// AlgoStatus 可能值: "NEW", "WORKING", "CANCELED", "TRIGGERED", "FILLED"
if algoOrder.AlgoStatus == "FILLED" || algoOrder.ActualOrderId != "" {
    closePrice, _ := parseFloat(algoOrder.ActualPrice)  // ✅ 使用 ActualPrice
    // ...
}
```

## 关键变更点总结

| 方面 | 旧实现 | 新实现 (Algo Order API) |
|------|--------|-------------------------|
| **服务类** | `NewCreateOrderService()` | `NewCreateAlgoOrderService()` |
| **订单类型** | `OrderTypeStopMarket` | `AlgoOrderTypeStopMarket` |
| **价格参数** | `StopPrice()` | `TriggerPrice()` |
| **订单 ID** | `order.OrderID` | `order.AlgoId` |
| **取消服务** | `NewCancelOrderService()` | `NewCancelAlgoOrderService()` |
| **查询服务** | `NewGetOrderService()` | `NewGetAlgoOrderService()` |
| **状态字段** | `order.Status` | `algoOrder.AlgoStatus` |
| **成交价格** | `order.AvgPrice` | `algoOrder.ActualPrice` |
| **是否需要 Symbol** | 是 | 否（只需要 AlgoID） |

## Algo Order 状态说明

- **NEW**: 订单已创建，等待触发
- **WORKING**: 订单正在工作中
- **CANCELED**: 订单已取消
- **TRIGGERED**: 订单已触发，实际市价单已下达
- **FILLED**: 订单已成交

当 `AlgoStatus == "FILLED"` 或 `ActualOrderId != ""` 时，说明止损单已触发并成交。

## 测试建议

### 1. 在测试网测试

确保 `.env` 文件中:
```bash
BINANCE_TEST_MODE=true
```

### 2. 验证止损单流程

1. 开仓
2. 检查是否成功下达初始止损单（日志中应显示 "止损单已下达(Algo)"）
3. 手动修改价格触发止损（测试网）
4. 验证止损单是否正确执行

### 3. 验证更新止损单流程

1. 开仓后触发追踪止损更新
2. 检查是否先取消旧止损单，再下达新止损单
3. 日志中应显示 "旧止损单已取消(Algo)"

## 部署步骤

### 1. 本地编译

```bash
make build
# 或
go build -o bin/trading-bot cmd/main.go
```

### 2. 上传到服务器

```bash
# 停止旧程序
# 上传新的二进制文件
# 重启程序
```

### 3. 监控日志

关注以下日志信息:
- ✅ `止损单已下达(Algo)` - 下单成功
- ✅ `旧止损单已取消(Algo)` - 取消成功
- ❌ `下初始止损单失败` - 下单失败（不应再出现 -4120 错误）

## 向后兼容性

**重要**: 此修复不向后兼容！

- 旧版本下达的止损单使用旧的 Order API
- 新版本只能管理 Algo Order API 下达的止损单
- 升级前建议:
  1. 关闭所有持仓
  2. 取消所有挂单
  3. 部署新版本
  4. 重新开始交易

## 相关文件

- `internal/executors/stoploss_manager.go` - 止损管理器（主要修改）
- `go.mod` - go-binance 依赖升级到 v2.8.9
- `go.sum` - 依赖哈希更新

## 参考资料

- [币安期货 Algo Order API 文档](https://developers.binance.com/docs/derivatives/usds-margined-futures/algo-orders)
- [go-binance PR #784](https://github.com/adshao/go-binance/pull/784) - Algo Order API 支持

## 常见问题

### Q1: 为什么要升级 SDK？

**A**: Algo Order API 支持在 go-binance v2.8.9 中添加，v2.8.7 不支持。

### Q2: 旧版本下达的止损单会怎样？

**A**: 新版本无法管理旧版本下达的止损单。建议升级前关闭所有持仓。

### Q3: 如果测试网测试失败怎么办？

**A**:
1. 检查 go-binance 版本是否为 v2.8.9+
2. 检查日志中的错误详情
3. 确认测试网 API 支持 Algo Order API

### Q4: Algo Order API 有什么优势？

**A**:
- 统一的条件单管理接口
- 更好的订单状态跟踪
- 服务器端执行，更可靠

## 风险提示

⚠️ **在正式网络部署前，务必在测试网充分测试！**

- 验证开仓后止损单能正常下达
- 验证止损单能正常触发执行
- 验证追踪止损能正常更新
- 验证手动取消止损单功能

---

**修复日期**: 2025-12-10
**修复版本**: go-binance v2.8.9
**修复人员**: Claude Code
