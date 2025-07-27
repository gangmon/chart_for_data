# ClickHouse Market Data Chart

这是一个用Go语言编写的程序，用于查询ClickHouse数据库中的金融市场数据，并在终端中显示价格和持仓量的图表。

## 功能特点

- 连接到ClickHouse数据库 (xm.local:8123)
- 查询feature.jm表中的市场数据
- 在终端中显示双线图表：
  - 绿色线：价格 (price)
  - 红色线：持仓量 (open_interest, 已标准化)
- 显示统计信息：平均价格、最高/最低价格、平均持仓量等
- 支持终端窗口大小调整

## 系统要求

- Go 1.23.0 或更高版本
- 能够访问 xm.local:8123 的ClickHouse服务器
- 终端支持（用于图表显示）

## 安装和运行

1. 确保Go环境已安装
2. 克隆或下载项目文件
3. 在项目目录中运行：

```bash
# 安装依赖
go mod download

# 编译程序
go build -o market-chart

# 运行程序
./market-chart
```

或者直接运行：

```bash
go run main.go
```

## 使用说明

1. 程序启动后会首先测试与ClickHouse的连接
2. 成功连接后，查询最近100条记录
3. 显示图表界面，包含：
   - 价格和持仓量的双线图
   - 图例说明
   - 统计信息面板
4. 操作说明：
   - 按 'q' 键或 Ctrl+C 退出程序
   - 终端窗口大小调整时图表会自动适应

## 数据库配置

程序连接的ClickHouse配置：
- 主机：xm.local
- 端口：8123 (HTTP接口)
- 数据库：feature
- 表：jm

## 表结构

程序期望的表结构如下：

```sql
CREATE TABLE feature.jm
(
    `symbol` String,
    `time` DateTime,
    `price` Float32,
    `vol` UInt32,
    `open_interest` UInt32,
    `diff_vol` Int32,
    `diff_oi` Int32,
    `bid_1` Float32,
    `bid_volumn_1` UInt32,
    `ask_1` Float32,
    `ask_volumn_1` UInt32,
    `datetime` UInt64
)
ENGINE = ReplacingMergeTree
ORDER BY (time, symbol, price, bid_1, bid_volumn_1, ask_1, ask_volumn_1, vol)
SETTINGS index_granularity = 8192
```

## 技术实现

- 使用HTTP接口连接ClickHouse，避免复杂的驱动依赖
- 使用termui库创建终端图表界面
- 持仓量数据经过标准化处理，映射到价格数据范围内便于在同一图表中显示
- 支持实时窗口大小调整

## 故障排除

1. **连接失败**：检查ClickHouse服务是否运行，主机名xm.local是否可访问
2. **无数据**：确认feature.jm表中有数据
3. **图表显示异常**：确保终端支持UTF-8和颜色显示

## 依赖项

- `github.com/gizak/termui/v3` - 终端UI库，用于创建图表
- Go标准库：net/http, time, strconv等
