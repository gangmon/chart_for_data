package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	Symbol       string    `json:"symbol"`
	Time         time.Time `json:"time"`
	Price        float32   `json:"price"`
	Vol          uint32    `json:"vol"`
	OpenInterest uint32    `json:"open_interest"`
	DiffVol      int32     `json:"diff_vol"`
	DiffOI       int32     `json:"diff_oi"`
	Bid1         float32   `json:"bid_1"`
	BidVolumn1   uint32    `json:"bid_volumn_1"`
	Ask1         float32   `json:"ask_1"`
	AskVolumn1   uint32    `json:"ask_volumn_1"`
	DateTime     uint64    `json:"datetime"`
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
			Time:         parsedTime,
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
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>JM2509 实时市场数据图表</h1>
            <p>JavaScript交互式图表 - 支持缩放、平移和详细数据查看</p>
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

        // 刷新数据
        function refreshData() {
            updateChart();
        }

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
		xValues[i] = record.Time
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
	webDataMutex.RLock()
	data := webCurrentData
	webDataMutex.RUnlock()

	if len(data) == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "No data available",
		})
		return
	}

	// 计算统计信息
	priceValues := make([]float64, len(data))
	oiValues := make([]float64, len(data))

	for i, record := range data {
		priceValues[i] = float64(record.Price)
		oiValues[i] = float64(record.OpenInterest)
	}

	stats := map[string]interface{}{
		"avg_price":     webCalculateAverage(priceValues),
		"max_price":     webFindMax(priceValues),
		"min_price":     webFindMin(priceValues),
		"avg_oi":        webCalculateAverage(oiValues),
		"data_points":   len(data),
		"total_records": len(webAllData),
	}

	response := map[string]interface{}{
		"data":      data,
		"stats":     stats,
		"timestamp": time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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
	max := data[0]
	for _, val := range data {
		if val > max {
			max = val
		}
	}
	return max
}

func webFindMin(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	min := data[0]
	for _, val := range data {
		if val < min {
			min = val
		}
	}
	return min
}

func webCalculateAverage(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	sum := 0.0
	for _, val := range data {
		sum += val
	}
	return sum / float64(len(data))
}
