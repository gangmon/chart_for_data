package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"
)

const (
	WEB_PORT = ":8082"
)

type WebMarketData struct {
	Symbol       string  `json:"symbol"`
	Time         string  `json:"time"`
	Price        float32 `json:"price"`
	Vol          uint32  `json:"vol"`
	OpenInterest uint32  `json:"open_interest"`
	DiffVol      int32   `json:"diff_vol"`
	DiffOI       int32   `json:"diff_oi"`
	Bid1         float32 `json:"bid_1"`
	BidVolumn1   uint32  `json:"bid_volumn_1"`
	Ask1         float32 `json:"ask_1"`
	AskVolumn1   uint32  `json:"ask_volumn_1"`
	DateTime     uint64  `json:"datetime"`
}

var (
	webAllData     []WebMarketData
	webCurrentData []WebMarketData
	webDataMutex   sync.RWMutex
)

func main() {
	fmt.Println("Connecting to ClickHouse...")

	// 测试连接
	if err := webTestConnection(); err != nil {
		log.Fatal("Failed to connect to ClickHouse:", err)
	}

	fmt.Println("Successfully connected to ClickHouse!")

	// 查询数据
	data, err := webQueryMarketData()
	if err != nil {
		log.Fatal("Failed to query data:", err)
	}

	if len(data) == 0 {
		log.Fatal("No data found in the table")
	}

	fmt.Printf("Found %d records\n", len(data))

	// 初始化全局数据
	webAllData = data

	// 对数据进行采样以便在浏览器中显示
	sampleSize := 100 // 减少到100条记录确保JSON响应不会太大
	webDataMutex.Lock()
	if len(webAllData) > sampleSize {
		// 均匀采样
		step := len(webAllData) / sampleSize
		webCurrentData = make([]WebMarketData, 0, sampleSize)
		for i := 0; i < len(webAllData); i += step {
			webCurrentData = append(webCurrentData, webAllData[i])
		}
		fmt.Printf("Sampled %d records from %d total records (every %d records) for display\n",
			len(webCurrentData), len(webAllData), step)
	} else {
		webCurrentData = webAllData
		fmt.Printf("Displaying all %d records in a single view\n", len(webAllData))
	}
	webDataMutex.Unlock()

	// 启动Web服务器
	webStartWebServer()
}

func webTestConnection() error {
	query := "SELECT 1"
	_, err := webExecuteQuery(query)
	return err
}

func webExecuteQuery(query string) (string, error) {
	// 构建请求URL
	baseURL := "http://xm.local:8123"
	params := url.Values{}
	params.Add("database", "feature")
	params.Add("query", query)

	fullURL := fmt.Sprintf("%s/?%s", baseURL, params.Encode())

	// 发送HTTP请求
	resp, err := http.Get(fullURL)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ClickHouse error (status %d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return string(body), nil
}

func webQueryMarketData() ([]WebMarketData, error) {
	query := `
		SELECT 
			symbol, 
			time, 
			price, 
			vol, 
			open_interest, 
			diff_vol, 
			diff_oi, 
			bid_1, 
			bid_volumn_1, 
			ask_1, 
			ask_volumn_1, 
			datetime
		FROM feature.jm 
		WHERE symbol = 'jm2509'
		ORDER BY time ASC 
		FORMAT TabSeparated
	`

	result, err := webExecuteQuery(query)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	return webParseTabSeparatedData(result)
}

func webParseTabSeparatedData(data string) ([]WebMarketData, error) {
	lines := strings.Split(strings.TrimSpace(data), "\n")
	var marketData []WebMarketData

	for _, line := range lines {
		if line == "" {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) < 12 {
			continue
		}

		// 解析时间
		timeStr := fields[1]
		parsedTime, err := time.Parse("2006-01-02 15:04:05", timeStr)
		if err != nil {
			log.Printf("Failed to parse time %s: %v", timeStr, err)
			continue
		}

		// 解析价格
		price, err := strconv.ParseFloat(fields[2], 32)
		if err != nil {
			log.Printf("Failed to parse price %s: %v", fields[2], err)
			continue
		}

		// 解析成交量
		vol, err := strconv.ParseUint(fields[3], 10, 32)
		if err != nil {
			log.Printf("Failed to parse vol %s: %v", fields[3], err)
			continue
		}

		// 解析持仓量
		openInterest, err := strconv.ParseUint(fields[4], 10, 32)
		if err != nil {
			log.Printf("Failed to parse open_interest %s: %v", fields[4], err)
			continue
		}

		// 解析其他字段
		diffVol, _ := strconv.ParseInt(fields[5], 10, 32)
		diffOI, _ := strconv.ParseInt(fields[6], 10, 32)
		bid1, _ := strconv.ParseFloat(fields[7], 32)
		bidVolumn1, _ := strconv.ParseUint(fields[8], 10, 32)
		ask1, _ := strconv.ParseFloat(fields[9], 32)
		askVolumn1, _ := strconv.ParseUint(fields[10], 10, 32)
		datetime, _ := strconv.ParseUint(fields[11], 10, 64)

		md := WebMarketData{
			Symbol:       fields[0],
			Time:         parsedTime.Format("2006-01-02 15:04:05"),
			Price:        float32(price),
			Vol:          uint32(vol),
			OpenInterest: uint32(openInterest),
			DiffVol:      int32(diffVol),
			DiffOI:       int32(diffOI),
			Bid1:         float32(bid1),
			BidVolumn1:   uint32(bidVolumn1),
			Ask1:         float32(ask1),
			AskVolumn1:   uint32(askVolumn1),
			DateTime:     datetime,
		}

		marketData = append(marketData, md)
	}

	return marketData, nil
}

// Web服务器
func webStartWebServer() {
	http.HandleFunc("/", webIndexHandler)
	http.HandleFunc("/chart", webChartHandler)
	http.HandleFunc("/data", webDataHandler)
	http.HandleFunc("/tables", webTablesHandler)
	http.HandleFunc("/symbols", webSymbolsHandler)

	fmt.Printf("\n\nStarting web server at http://localhost%s\n", WEB_PORT)
	fmt.Println("Open your browser and visit the URL above to view the chart")
	fmt.Println("Direct chart access: http://localhost" + WEB_PORT + "/chart")

	log.Fatal(http.ListenAndServe(WEB_PORT, nil))
}

// 主页处理器 - 显示JavaScript图表页面
func webIndexHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := `
<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>JM2509 Interactive Chart</title>
    <script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.js"></script>
    <script src="https://cdn.jsdelivr.net/npm/chartjs-plugin-zoom@2.0.1/dist/chartjs-plugin-zoom.min.js"></script>
    <style>
        body { 
            font-family: Arial, sans-serif; 
            margin: 20px; 
            background-color: #f5f5f5;
        }
        .container { 
            max-width: 1600px; 
            margin: 0 auto; 
            background-color: white;
            padding: 20px;
            border-radius: 8px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
        }
        .header {
            text-align: center;
            margin-bottom: 20px;
            color: #333;
        }
        .stats {
            display: flex;
            justify-content: space-around;
            margin-bottom: 20px;
            padding: 15px;
            background-color: #f8f9fa;
            border-radius: 5px;
        }
        .stat-item {
            text-align: center;
        }
        .stat-value {
            font-size: 1.5em;
            font-weight: bold;
            color: #007bff;
        }
        .stat-label {
            font-size: 0.9em;
            color: #666;
        }
        #chartContainer {
            position: relative;
            height: 700px;
            margin-bottom: 20px;
        }
        .controls {
            text-align: center;
            margin-bottom: 20px;
        }
        button {
            padding: 10px 20px;
            margin: 0 5px;
            border: none;
            border-radius: 5px;
            background-color: #007bff;
            color: white;
            cursor: pointer;
            font-size: 14px;
        }
        button:hover {
            background-color: #0056b3;
        }
        .status {
            text-align: center;
            padding: 10px;
            background-color: #d4edda;
            border: 1px solid #c3e6cb;
            border-radius: 5px;
            color: #155724;
            margin-top: 20px;
        }
        .info {
            background-color: #e7f3ff;
            border: 1px solid #bee5eb;
            border-radius: 5px;
            padding: 15px;
            margin: 20px 0;
            text-align: center;
        }
        .query-controls {
            display: flex;
            justify-content: center;
            align-items: center;
            gap: 20px;
            margin-bottom: 20px;
            padding: 15px;
            background-color: #f8f9fa;
            border-radius: 5px;
            border: 1px solid #dee2e6;
        }
        .control-group {
            display: flex;
            flex-direction: column;
            align-items: center;
        }
        .control-group label {
            font-weight: bold;
            margin-bottom: 5px;
            color: #495057;
            font-size: 14px;
        }
        .control-group select,
        .control-group input {
            padding: 8px 12px;
            border: 1px solid #ced4da;
            border-radius: 4px;
            font-size: 14px;
            min-width: 150px;
        }
        .control-group select:focus,
        .control-group input:focus {
            outline: none;
            border-color: #007bff;
            box-shadow: 0 0 0 2px rgba(0,123,255,0.25);
        }
        .query-btn {
            background-color: #28a745 !important;
            padding: 8px 20px !important;
            margin-top: 20px !important;
        }
        .query-btn:hover {
            background-color: #218838 !important;
        }
        .input-mode-toggle {
            margin-top: 5px;
            font-size: 12px;
        }
        .input-mode-toggle label {
            display: flex;
            align-items: center;
            gap: 5px;
            font-weight: normal !important;
            cursor: pointer;
        }
        .input-mode-toggle input[type="checkbox"] {
            width: auto;
            min-width: auto;
        }
        .error-message {
            background-color: #f8d7da;
            border: 1px solid #f5c6cb;
            color: #721c24;
            padding: 10px;
            border-radius: 4px;
            margin-top: 10px;
            text-align: center;
            font-weight: bold;
        }
        .success-message {
            background-color: #d4edda;
            border: 1px solid #c3e6cb;
            color: #155724;
            padding: 10px;
            border-radius: 4px;
            margin-top: 10px;
            text-align: center;
            font-weight: bold;
        }
        datalist {
            background-color: white;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>实时市场数据图表</h1>
            <p>JavaScript交互式图表 - 支持缩放、平移和详细数据查看</p>
        </div>
        
        <div class="query-controls">
            <div class="control-group">
                <label for="tableInput">数据表名:</label>
                <input type="text" id="tableInput" placeholder="例如: jm, SA, MA, rb" list="tableList">
                <datalist id="tableList">
                    <!-- 动态加载的表选项 -->
                </datalist>
                <div class="input-mode-toggle">
                    <label>
                        <input type="checkbox" id="tableDropdownMode"> 使用下拉选择
                    </label>
                </div>
                <select id="tableSelect" style="display: none;">
                    <option value="">正在加载...</option>
                </select>
            </div>
            <div class="control-group">
                <label for="symbolInput">Symbol代码:</label>
                <input type="text" id="symbolInput" placeholder="例如: jm2509, SA509, MA509" list="symbolList">
                <datalist id="symbolList">
                    <!-- 动态加载的symbol选项 -->
                </datalist>
                <div class="input-mode-toggle">
                    <label>
                        <input type="checkbox" id="symbolDropdownMode"> 使用下拉选择
                    </label>
                </div>
                <select id="symbolSelect" style="display: none;">
                    <option value="">请先选择数据表</option>
                </select>
            </div>
            <div class="control-group">
                <button onclick="queryData()" class="query-btn">查询数据</button>
            </div>
            <div class="error-message" id="errorMessage" style="display: none;"></div>
        </div>
        
        <div class="stats" id="stats">
            <div class="stat-item">
                <div class="stat-value" id="avgPrice">--</div>
                <div class="stat-label">平均价格</div>
            </div>
            <div class="stat-item">
                <div class="stat-value" id="maxPrice">--</div>
                <div class="stat-label">最高价格</div>
            </div>
            <div class="stat-item">
                <div class="stat-value" id="minPrice">--</div>
                <div class="stat-label">最低价格</div>
            </div>
            <div class="stat-item">
                <div class="stat-value" id="avgOI">--</div>
                <div class="stat-label">平均持仓量</div>
            </div>
            <div class="stat-item">
                <div class="stat-value" id="dataPoints">--</div>
                <div class="stat-label">数据点数</div>
            </div>
        </div>

        <div class="info">
            <p><strong>操作说明：</strong></p>
            <p>• 鼠标滚轮：缩放图表 | 拖拽：平移查看不同时间段 | 双击：重置缩放</p>
            <p>• 点击图例：显示/隐藏对应数据线 | 悬停：查看详细数据</p>
        </div>

        <div class="controls">
            <button onclick="resetZoom()">重置缩放</button>
            <button onclick="zoomIn()">放大</button>
            <button onclick="zoomOut()">缩小</button>
            <button onclick="togglePrice()">显示/隐藏价格</button>
            <button onclick="toggleOI()">显示/隐藏持仓量</button>
            <button onclick="refreshData()">刷新数据</button>
        </div>

        <div id="chartContainer">
            <canvas id="myChart"></canvas>
        </div>

        <div class="status" id="status">
            正在加载数据...
        </div>
    </div>

    <script>
        let chart;
        let chartData = null;

        // 初始化图表
        function initChart() {
            // 注册缩放插件
            Chart.register(ChartZoom);
            
            const ctx = document.getElementById('myChart').getContext('2d');
            chart = new Chart(ctx, {
                type: 'line',
                data: {
                    labels: [],
                    datasets: [{
                        label: '价格',
                        data: [],
                        borderColor: '#28a745',
                        backgroundColor: 'rgba(40, 167, 69, 0.1)',
                        tension: 0.1,
                        yAxisID: 'y',
                        pointRadius: 0,
                        pointHoverRadius: 4,
                        borderWidth: 2
                    }, {
                        label: '持仓量',
                        data: [],
                        borderColor: '#dc3545',
                        backgroundColor: 'rgba(220, 53, 69, 0.1)',
                        tension: 0.1,
                        yAxisID: 'y1',
                        pointRadius: 0,
                        pointHoverRadius: 4,
                        borderWidth: 2
                    }]
                },
                options: {
                    responsive: true,
                    maintainAspectRatio: false,
                    interaction: {
                        mode: 'index',
                        intersect: false,
                    },
                    scales: {
                        x: {
                            display: true,
                            title: {
                                display: true,
                                text: '日期时间',
                                font: {
                                    size: 14
                                }
                            },
                            ticks: {
                                font: {
                                    size: 12
                                }
                            }
                        },
                        y: {
                            type: 'linear',
                            display: true,
                            position: 'left',
                            title: {
                                display: true,
                                text: '价格',
                                font: {
                                    size: 14
                                }
                            },
                            ticks: {
                                font: {
                                    size: 12
                                }
                            }
                        },
                        y1: {
                            type: 'linear',
                            display: true,
                            position: 'right',
                            title: {
                                display: true,
                                text: '持仓量',
                                font: {
                                    size: 14
                                }
                            },
                            grid: {
                                drawOnChartArea: false,
                            },
                            ticks: {
                                font: {
                                    size: 12
                                }
                            }
                        }
                    },
                    plugins: {
                        legend: {
                            display: true,
                            position: 'top',
                            labels: {
                                font: {
                                    size: 14
                                }
                            }
                        },
                        title: {
                            display: true,
                            text: 'JM2509 交互式数据图表',
                            font: {
                                size: 16
                            }
                        },
                        zoom: {
                            pan: {
                                enabled: true,
                                mode: 'x'
                            },
                            zoom: {
                                wheel: {
                                    enabled: true,
                                },
                                pinch: {
                                    enabled: true
                                },
                                mode: 'x',
                            }
                        }
                    },
                    elements: {
                        point: {
                            radius: 0
                        }
                    }
                }
            });
        }

        // 更新图表数据
        function updateChart() {
            document.getElementById('status').textContent = '正在加载数据...';
            
            fetch('/data')
                .then(response => {
                    if (!response.ok) {
                        throw new Error('Network response was not ok');
                    }
                    return response.json();
                })
                .then(data => {
                    if (data.error) {
                        document.getElementById('status').textContent = '错误: ' + data.error;
                        return;
                    }

                    chartData = data;

                    // 更新图表数据
                    const labels = data.data.map(item => {
                        const date = new Date(item.time);
                        return date.toLocaleDateString('zh-CN', {
                            month: '2-digit',
                            day: '2-digit',
                            hour: '2-digit',
                            minute: '2-digit'
                        });
                    });
                    
                    const prices = data.data.map(item => item.price);
                    const openInterests = data.data.map(item => item.open_interest);

                    chart.data.labels = labels;
                    chart.data.datasets[0].data = prices;
                    chart.data.datasets[1].data = openInterests;
                    chart.update('none');

                    // 更新统计信息
                    updateStats(data.stats);
                    
                    // 更新状态
                    document.getElementById('status').textContent = 
                        '数据加载完成 | 最后更新: ' + new Date().toLocaleTimeString() + 
                        ' | 显示 ' + data.stats.data_points + ' 条采样数据，共 ' + data.stats.total_records + ' 条原始记录';
                })
                .catch(error => {
                    console.error('Error:', error);
                    document.getElementById('status').textContent = '数据获取失败: ' + error.message;
                });
        }

        // 更新统计信息
        function updateStats(stats) {
            document.getElementById('avgPrice').textContent = stats.avg_price.toFixed(2);
            document.getElementById('maxPrice').textContent = stats.max_price.toFixed(2);
            document.getElementById('minPrice').textContent = stats.min_price.toFixed(2);
            document.getElementById('avgOI').textContent = Math.round(stats.avg_oi).toLocaleString();
            document.getElementById('dataPoints').textContent = stats.data_points.toLocaleString();
        }

        // 缩放功能
        function zoomIn() {
            chart.zoom(1.2);
        }

        function zoomOut() {
            chart.zoom(0.8);
        }

        function resetZoom() {
            chart.resetZoom();
        }

        // 切换数据显示
        function togglePrice() {
            const dataset = chart.data.datasets[0];
            dataset.hidden = !dataset.hidden;
            chart.update();
        }

        function toggleOI() {
            const dataset = chart.data.datasets[1];
            dataset.hidden = !dataset.hidden;
            chart.update();
        }

        // 显示错误消息
        function showError(message) {
            const errorDiv = document.getElementById('errorMessage');
            errorDiv.textContent = message;
            errorDiv.style.display = 'block';
            errorDiv.className = 'error-message';
            setTimeout(() => {
                errorDiv.style.display = 'none';
            }, 5000);
        }

        // 显示成功消息
        function showSuccess(message) {
            const errorDiv = document.getElementById('errorMessage');
            errorDiv.textContent = message;
            errorDiv.style.display = 'block';
            errorDiv.className = 'success-message';
            setTimeout(() => {
                errorDiv.style.display = 'none';
            }, 3000);
        }

        // 加载所有表
        function loadTables() {
            fetch('/tables')
                .then(response => response.json())
                .then(data => {
                    if (data.error) {
                        console.error('加载表失败:', data.error);
                        return;
                    }
                    
                    // 更新下拉框
                    const tableSelect = document.getElementById('tableSelect');
                    tableSelect.innerHTML = '<option value="">请选择数据表</option>';
                    
                    // 更新datalist
                    const tableList = document.getElementById('tableList');
                    tableList.innerHTML = '';
                    
                    data.tables.forEach(table => {
                        // 下拉框选项
                        const option = document.createElement('option');
                        option.value = table;
                        option.textContent = table.toUpperCase();
                        tableSelect.appendChild(option);
                        
                        // datalist选项
                        const dataOption = document.createElement('option');
                        dataOption.value = table;
                        tableList.appendChild(dataOption);
                    });
                })
                .catch(error => {
                    console.error('加载表失败:', error);
                    showError('加载表列表失败: ' + error.message);
                });
        }
        
        // 加载指定表的symbols
        function loadSymbols(table) {
            if (!table) {
                const symbolSelect = document.getElementById('symbolSelect');
                symbolSelect.innerHTML = '<option value="">请先选择数据表</option>';
                const symbolList = document.getElementById('symbolList');
                symbolList.innerHTML = '';
                return;
            }
            
            const symbolSelect = document.getElementById('symbolSelect');
            symbolSelect.innerHTML = '<option value="">正在加载...</option>';
            
            fetch('/symbols?table=' + encodeURIComponent(table))
                .then(response => response.json())
                .then(data => {
                    if (data.error) {
                        symbolSelect.innerHTML = '<option value="">加载失败</option>';
                        console.error('加载symbols失败:', data.error);
                        showError('加载symbols失败: ' + data.error);
                        return;
                    }
                    
                    // 更新下拉框
                    symbolSelect.innerHTML = '<option value="">请选择Symbol</option>';
                    
                    // 更新datalist
                    const symbolList = document.getElementById('symbolList');
                    symbolList.innerHTML = '';
                    
                    data.symbols.forEach(symbol => {
                        // 下拉框选项
                        const option = document.createElement('option');
                        option.value = symbol;
                        option.textContent = symbol;
                        symbolSelect.appendChild(option);
                        
                        // datalist选项
                        const dataOption = document.createElement('option');
                        dataOption.value = symbol;
                        symbolList.appendChild(dataOption);
                    });
                })
                .catch(error => {
                    symbolSelect.innerHTML = '<option value="">加载失败</option>';
                    console.error('加载symbols失败:', error);
                    showError('加载symbols失败: ' + error.message);
                });
        }

        // 获取当前输入的表名和symbol
        function getCurrentInputs() {
            const tableDropdownMode = document.getElementById('tableDropdownMode').checked;
            const symbolDropdownMode = document.getElementById('symbolDropdownMode').checked;
            
            const table = tableDropdownMode ? 
                document.getElementById('tableSelect').value : 
                document.getElementById('tableInput').value.trim();
                
            const symbol = symbolDropdownMode ? 
                document.getElementById('symbolSelect').value : 
                document.getElementById('symbolInput').value.trim();
                
            return { table, symbol };
        }

        // 查询数据
        function queryData() {
            const { table, symbol } = getCurrentInputs();
            
            if (!table) {
                showError('请输入或选择数据表名');
                return;
            }
            
            if (!symbol) {
                showError('请输入或选择Symbol代码');
                return;
            }
            
            document.getElementById('status').textContent = '正在查询数据...';
            
            // 更新图表标题
            chart.options.plugins.title.text = symbol.toUpperCase() + ' 交互式数据图表';
            chart.update('none');
            
            // 发送查询请求
            fetch('/data?table=' + encodeURIComponent(table) + '&symbol=' + encodeURIComponent(symbol))
                .then(response => {
                    if (!response.ok) {
                        throw new Error('Network response was not ok');
                    }
                    return response.json();
                })
                .then(data => {
                    if (data.error) {
                        showError(data.error);
                        document.getElementById('status').textContent = '查询失败';
                        return;
                    }

                    chartData = data;

                    // 更新图表数据
                    const labels = data.data.map(item => {
                        const date = new Date(item.time);
                        return date.toLocaleDateString('zh-CN', {
                            month: '2-digit',
                            day: '2-digit',
                            hour: '2-digit',
                            minute: '2-digit'
                        });
                    });
                    
                    const prices = data.data.map(item => item.price);
                    const openInterests = data.data.map(item => item.open_interest);

                    chart.data.labels = labels;
                    chart.data.datasets[0].data = prices;
                    chart.data.datasets[1].data = openInterests;
                    chart.update('none');

                    // 更新统计信息
                    updateStats(data.stats);
                    
                    // 显示成功消息
                    showSuccess('数据查询成功！');
                    
                    // 更新状态
                    document.getElementById('status').textContent = 
                        '数据查询完成 | 表: ' + table.toUpperCase() + ' | Symbol: ' + symbol.toUpperCase() + ' | 最后更新: ' + new Date().toLocaleTimeString() + 
                        ' | 显示 ' + data.stats.data_points + ' 条采样数据，共 ' + data.stats.total_records + ' 条原始记录';
                })
                .catch(error => {
                    console.error('Error:', error);
                    showError('数据查询失败: ' + error.message);
                    document.getElementById('status').textContent = '查询失败';
                });
        }

        // 刷新数据
        function refreshData() {
            const { table, symbol } = getCurrentInputs();
            if (table && symbol) {
                queryData();
            } else {
                updateChart();
            }
        }
        
        // 切换输入模式
        function toggleInputMode(type) {
            const isTable = type === 'table';
            const checkbox = document.getElementById(isTable ? 'tableDropdownMode' : 'symbolDropdownMode');
            const input = document.getElementById(isTable ? 'tableInput' : 'symbolInput');
            const select = document.getElementById(isTable ? 'tableSelect' : 'symbolSelect');
            
            if (checkbox.checked) {
                input.style.display = 'none';
                select.style.display = 'block';
            } else {
                input.style.display = 'block';
                select.style.display = 'none';
            }
        }
        
        // 同步输入框和下拉框的值
        function syncInputValues(type) {
            const isTable = type === 'table';
            const checkbox = document.getElementById(isTable ? 'tableDropdownMode' : 'symbolDropdownMode');
            const input = document.getElementById(isTable ? 'tableInput' : 'symbolInput');
            const select = document.getElementById(isTable ? 'tableSelect' : 'symbolSelect');
            
            if (checkbox.checked) {
                // 从输入框同步到下拉框
                const inputValue = input.value.trim();
                if (inputValue) {
                    // 查找匹配的选项
                    for (let option of select.options) {
                        if (option.value === inputValue) {
                            select.value = inputValue;
                            break;
                        }
                    }
                }
            } else {
                // 从下拉框同步到输入框
                if (select.value) {
                    input.value = select.value;
                }
            }
        }
        
        // 表输入变化时自动加载symbols
        function handleTableInputChange() {
            const tableDropdownMode = document.getElementById('tableDropdownMode').checked;
            const table = tableDropdownMode ? 
                document.getElementById('tableSelect').value : 
                document.getElementById('tableInput').value.trim();
            
            if (table) {
                loadSymbols(table);
            }
        }
        
        // 事件监听器
        document.getElementById('tableDropdownMode').addEventListener('change', function() {
            syncInputValues('table');
            toggleInputMode('table');
        });
        
        document.getElementById('symbolDropdownMode').addEventListener('change', function() {
            syncInputValues('symbol');
            toggleInputMode('symbol');
        });
        
        document.getElementById('tableSelect').addEventListener('change', handleTableInputChange);
        document.getElementById('tableInput').addEventListener('input', handleTableInputChange);
        
        // 支持回车键查询
        document.getElementById('tableInput').addEventListener('keypress', function(event) {
            if (event.key === 'Enter') {
                queryData();
            }
        });
        
        document.getElementById('symbolInput').addEventListener('keypress', function(event) {
            if (event.key === 'Enter') {
                queryData();
            }
        });

        // 键盘快捷键
        document.addEventListener('keydown', function(event) {
            switch(event.key) {
                case '+':
                case '=':
                    zoomIn();
                    break;
                case '-':
                    zoomOut();
                    break;
                case '0':
                    resetZoom();
                    break;
                case 'r':
                case 'R':
                    refreshData();
                    break;
            }
        });

        // 页面加载完成后初始化
        window.onload = function() {
            initChart();
            loadTables();
            updateChart();
        };
    </script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(tmpl))
}

// 图表处理器 (生成PNG图表)
func webChartHandler(w http.ResponseWriter, r *http.Request) {
	webDataMutex.RLock()
	data := webCurrentData
	webDataMutex.RUnlock()

	if len(data) < 2 {
		http.Error(w, "Insufficient data", http.StatusInternalServerError)
		return
	}

	// 准备数据
	xValues := make([]time.Time, len(data))
	priceValues := make([]float64, len(data))
	oiValues := make([]float64, len(data))

	for i, record := range data {
		// 解析时间字符串
		parsedTime, err := time.Parse("2006-01-02 15:04:05", record.Time)
		if err != nil {
			log.Printf("Failed to parse time %s: %v", record.Time, err)
			continue
		}
		xValues[i] = parsedTime
		priceValues[i] = float64(record.Price)
		oiValues[i] = float64(record.OpenInterest)
	}

	// 计算统计信息
	avgPrice := webCalculateAverage(priceValues)
	maxPrice := webFindMax(priceValues)
	minPrice := webFindMin(priceValues)
	avgOI := webCalculateAverage(oiValues)

	// 创建图表
	graph := chart.Chart{
		Title: fmt.Sprintf("JM2509 - 全数据视图 (%d条采样数据，共%d条记录)\n平均价格: %.2f | 最高: %.2f | 最低: %.2f | 平均持仓量: %.0f",
			len(data), len(webAllData), avgPrice, maxPrice, minPrice, avgOI),
		TitleStyle: chart.Style{
			FontSize: 14,
		},
		Width:  1400,
		Height: 800,
		Background: chart.Style{
			Padding: chart.Box{
				Top:    80,
				Left:   80,
				Right:  80,
				Bottom: 80,
			},
		},
		XAxis: chart.XAxis{
			Name: "日期时间",
			Style: chart.Style{
				FontSize: 12,
			},
			ValueFormatter: chart.TimeValueFormatterWithFormat("01-02 15:04"),
		},
		YAxis: chart.YAxis{
			Name: "价格",
			Style: chart.Style{
				FontSize: 12,
			},
		},
		YAxisSecondary: chart.YAxis{
			Name: "持仓量",
			Style: chart.Style{
				FontSize: 12,
			},
		},
		Series: []chart.Series{
			chart.TimeSeries{
				Name: "价格",
				Style: chart.Style{
					StrokeColor: drawing.ColorGreen,
					StrokeWidth: 2,
				},
				XValues: xValues,
				YValues: priceValues,
			},
			chart.TimeSeries{
				Name: "持仓量",
				Style: chart.Style{
					StrokeColor: drawing.ColorRed,
					StrokeWidth: 2,
				},
				YAxis:   chart.YAxisSecondary,
				XValues: xValues,
				YValues: oiValues,
			},
		},
	}

	// 添加图例
	graph.Elements = []chart.Renderable{
		chart.Legend(&graph),
	}

	w.Header().Set("Content-Type", "image/png")
	err := graph.Render(chart.PNG, w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// 数据API处理器
func webDataHandler(w http.ResponseWriter, r *http.Request) {
	// 获取查询参数
	table := r.URL.Query().Get("table")
	symbol := r.URL.Query().Get("symbol")

	// 如果有查询参数，执行动态查询
	if table != "" && symbol != "" {
		data, err := webQueryMarketDataDynamic(table, symbol)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": fmt.Sprintf("查询失败: %v", err),
			})
			return
		}

		if len(data) == 0 {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": fmt.Sprintf("未找到表 %s 中 symbol = %s 的数据", table, symbol),
			})
			return
		}

		// 更新全局数据
		webDataMutex.Lock()
		webAllData = data

		// 对数据进行采样
		sampleSize := 100
		if len(data) > sampleSize {
			step := len(data) / sampleSize
			webCurrentData = make([]WebMarketData, 0, sampleSize)
			for i := 0; i < len(data); i += step {
				webCurrentData = append(webCurrentData, data[i])
			}
		} else {
			webCurrentData = data
		}
		webDataMutex.Unlock()

		fmt.Printf("Dynamic query: table=%s, symbol=%s, found %d records, sampled %d\n",
			table, symbol, len(data), len(webCurrentData))
	}

	// 返回当前数据
	webDataMutex.RLock()
	data := webCurrentData
	allData := webAllData
	webDataMutex.RUnlock()

	fmt.Printf("Retrieved data: %d current, %d total\n", len(data), len(allData))

	if len(data) == 0 {
		fmt.Printf("No data available, returning error\n")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "No data available",
		})
		return
	}

	fmt.Printf("Data available, proceeding with stats calculation\n")

	// 计算统计信息
	priceValues := make([]float64, len(data))
	oiValues := make([]float64, len(data))

	fmt.Printf("Starting to calculate stats for %d data points\n", len(data))

	for i, record := range data {
		priceValues[i] = float64(record.Price)
		oiValues[i] = float64(record.OpenInterest)
	}

	fmt.Printf("Calculated price and OI values\n")

	avgPrice := webCalculateAverage(priceValues)
	maxPrice := webFindMax(priceValues)
	minPrice := webFindMin(priceValues)
	avgOI := webCalculateAverage(oiValues)

	// 确保所有统计值都是有效的
	if math.IsInf(avgPrice, 0) || math.IsNaN(avgPrice) {
		avgPrice = 0
	}
	if math.IsInf(maxPrice, 0) || math.IsNaN(maxPrice) {
		maxPrice = 0
	}
	if math.IsInf(minPrice, 0) || math.IsNaN(minPrice) {
		minPrice = 0
	}
	if math.IsInf(avgOI, 0) || math.IsNaN(avgOI) {
		avgOI = 0
	}

	stats := map[string]interface{}{
		"avg_price":     avgPrice,
		"max_price":     maxPrice,
		"min_price":     minPrice,
		"avg_oi":        avgOI,
		"data_points":   len(data),
		"total_records": len(allData),
	}

	fmt.Printf("Calculated stats: avg_price=%.2f, data_points=%d\n", avgPrice, len(data))

	// 过滤数据中的无穷大和NaN值，并创建清理后的数据
	cleanData := make([]WebMarketData, 0, len(data))
	for _, record := range data {
		// 创建一个新的记录，确保所有float字段都是有效的
		cleanRecord := record

		// 检查并清理Price字段
		if math.IsInf(float64(record.Price), 0) || math.IsNaN(float64(record.Price)) {
			cleanRecord.Price = 0
		}

		// 检查并清理Bid1字段
		if math.IsInf(float64(record.Bid1), 0) || math.IsNaN(float64(record.Bid1)) {
			cleanRecord.Bid1 = 0
		}

		// 检查并清理Ask1字段
		if math.IsInf(float64(record.Ask1), 0) || math.IsNaN(float64(record.Ask1)) {
			cleanRecord.Ask1 = 0
		}

		cleanData = append(cleanData, cleanRecord)
	}

	fmt.Printf("Cleaned data: %d records processed\n", len(cleanData))

	// 简化响应，避免time.Time可能的JSON编码问题
	response := map[string]interface{}{
		"data":      cleanData,
		"stats":     stats,
		"timestamp": time.Now().Format("2006-01-02 15:04:05"),
	}

	fmt.Printf("Created response object\n")

	w.Header().Set("Content-Type", "application/json")

	// 添加调试信息
	fmt.Printf("Encoding JSON response with %d data points\n", len(cleanData))

	// 使用自定义JSON编码来处理可能的无穷大值
	jsonBytes, err := json.Marshal(response)
	if err != nil {
		fmt.Printf("JSON encoding error: %v\n", err)
		// 如果JSON编码失败，返回一个简化的响应
		fallbackResponse := map[string]interface{}{
			"error": "数据包含无效值，无法序列化",
			"stats": map[string]interface{}{
				"data_points": len(cleanData),
				"message":     "请检查数据源",
			},
		}
		json.NewEncoder(w).Encode(fallbackResponse)
		return
	}

	// 检查JSON中是否包含无穷大值
	jsonStr := string(jsonBytes)
	if strings.Contains(jsonStr, "Infinity") || strings.Contains(jsonStr, "NaN") {
		fmt.Printf("JSON contains invalid values, returning error\n")
		fallbackResponse := map[string]interface{}{
			"error": "数据包含无穷大或NaN值",
			"stats": map[string]interface{}{
				"data_points": len(cleanData),
				"message":     "数据已被过滤",
			},
		}
		json.NewEncoder(w).Encode(fallbackResponse)
		return
	}

	w.Write(jsonBytes)
	fmt.Printf("JSON response sent successfully\n")
}

// 动态查询市场数据
func webQueryMarketDataDynamic(table, symbol string) ([]WebMarketData, error) {
	// 验证表名是否存在，防止SQL注入
	checkQuery := fmt.Sprintf("SELECT 1 FROM feature.%s LIMIT 1", table)
	_, err := webExecuteQuery(checkQuery)
	if err != nil {
		return nil, fmt.Errorf("表 %s 不存在或无法访问: %w", table, err)
	}

	query := fmt.Sprintf(`
		SELECT 
			symbol, 
			time, 
			price, 
			vol, 
			open_interest, 
			diff_vol, 
			diff_oi, 
			bid_1, 
			bid_volumn_1, 
			ask_1, 
			ask_volumn_1, 
			datetime
		FROM feature.%s 
		WHERE symbol = '%s'
		ORDER BY time ASC 
		FORMAT TabSeparated
	`, table, strings.ReplaceAll(symbol, "'", "''")) // 简单的SQL转义

	result, err := webExecuteQuery(query)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	return webParseTabSeparatedData(result)
}

func webNormalizeToRange(source, target []float64) []float64 {
	if len(source) == 0 || len(target) == 0 {
		return source
	}

	sourceMin := webFindMin(source)
	sourceMax := webFindMax(source)
	targetMin := webFindMin(target)
	targetMax := webFindMax(target)

	if sourceMax == sourceMin {
		return source
	}

	normalized := make([]float64, len(source))
	for i, val := range source {
		// 将source数据从[sourceMin, sourceMax]映射到[targetMin, targetMax]
		normalized[i] = targetMin + (val-sourceMin)*(targetMax-targetMin)/(sourceMax-sourceMin)
	}

	return normalized
}

func webFindMax(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}

	var max float64
	hasValidValue := false

	for _, val := range data {
		if !math.IsInf(val, 0) && !math.IsNaN(val) {
			if !hasValidValue || val > max {
				max = val
				hasValidValue = true
			}
		}
	}

	if !hasValidValue {
		return 0
	}
	return max
}

func webFindMin(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}

	var min float64
	hasValidValue := false

	for _, val := range data {
		if !math.IsInf(val, 0) && !math.IsNaN(val) {
			if !hasValidValue || val < min {
				min = val
				hasValidValue = true
			}
		}
	}

	if !hasValidValue {
		return 0
	}
	return min
}

func webCalculateAverage(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	sum := 0.0
	validCount := 0
	for _, val := range data {
		if !math.IsInf(val, 0) && !math.IsNaN(val) {
			sum += val
			validCount++
		}
	}
	if validCount == 0 {
		return 0
	}
	return sum / float64(validCount)
}

// 获取所有表的API处理器
func webTablesHandler(w http.ResponseWriter, r *http.Request) {
	query := "SHOW TABLES"
	result, err := webExecuteQuery(query)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("获取表列表失败: %v", err),
		})
		return
	}

	lines := strings.Split(strings.TrimSpace(result), "\n")
	var tables []string
	for _, line := range lines {
		if line != "" {
			tables = append(tables, strings.TrimSpace(line))
		}
	}

	response := map[string]interface{}{
		"tables": tables,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// 获取指定表的所有symbol的API处理器
func webSymbolsHandler(w http.ResponseWriter, r *http.Request) {
	table := r.URL.Query().Get("table")
	if table == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "缺少table参数",
		})
		return
	}

	// 验证表名是否存在，防止SQL注入
	checkQuery := fmt.Sprintf("SELECT 1 FROM feature.%s LIMIT 1", table)
	_, err := webExecuteQuery(checkQuery)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("表 %s 不存在或无法访问", table),
		})
		return
	}

	query := fmt.Sprintf("SELECT DISTINCT symbol FROM feature.%s ORDER BY symbol", table)
	result, err := webExecuteQuery(query)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("获取symbol列表失败: %v", err),
		})
		return
	}

	lines := strings.Split(strings.TrimSpace(result), "\n")
	var symbols []string
	for _, line := range lines {
		if line != "" {
			symbols = append(symbols, strings.TrimSpace(line))
		}
	}

	response := map[string]interface{}{
		"table":   table,
		"symbols": symbols,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
