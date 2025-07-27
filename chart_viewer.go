package main

import (
	"encoding/json"
	"fmt"
	"html/template"
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
	WINDOW_SIZE     = 1000
	UPDATE_INTERVAL = 2 * time.Second
	WEB_PORT        = ":8080"
)

type MarketData struct {
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
	allData     []MarketData
	currentData []MarketData
	dataMutex   sync.RWMutex
	windowStart int
)

func main() {
	fmt.Println("Connecting to ClickHouse...")

	// 测试连接
	if err := testConnection(); err != nil {
		log.Fatal("Failed to connect to ClickHouse:", err)
	}

	fmt.Println("Successfully connected to ClickHouse!")

	// 查询数据
	data, err := queryMarketData()
	if err != nil {
		log.Fatal("Failed to query data:", err)
	}

	if len(data) == 0 {
		log.Fatal("No data found in the table")
	}

	fmt.Printf("Found %d records\n", len(data))

	// 初始化全局数据
	allData = data
	windowStart = 0

	// 启动数据更新协程
	go updateDataLoop()

	// 启动Web服务器
	startWebServer()
}

func testConnection() error {
	query := "SELECT 1"
	_, err := executeQuery(query)
	return err
}

func executeQuery(query string) (string, error) {
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

func queryMarketData() ([]MarketData, error) {
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

	result, err := executeQuery(query)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	return parseTabSeparatedData(result)
}

func parseTabSeparatedData(data string) ([]MarketData, error) {
	lines := strings.Split(strings.TrimSpace(data), "\n")
	var marketData []MarketData

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

		md := MarketData{
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

// 数据更新循环
func updateDataLoop() {
	totalRecords := len(allData)

	for {
		// 获取当前窗口数据
		windowEnd := windowStart + WINDOW_SIZE
		if windowEnd > totalRecords {
			windowEnd = totalRecords
		}

		if windowStart >= totalRecords {
			windowStart = 0
			windowEnd = WINDOW_SIZE
			if windowEnd > totalRecords {
				windowEnd = totalRecords
			}
		}

		// 更新当前数据
		dataMutex.Lock()
		currentData = allData[windowStart:windowEnd]
		dataMutex.Unlock()

		if len(currentData) >= 2 {
			// 显示统计信息
			priceValues := make([]float64, len(currentData))
			oiValues := make([]float64, len(currentData))

			for i, record := range currentData {
				priceValues[i] = float64(record.Price)
				oiValues[i] = float64(record.OpenInterest)
			}

			avgPrice := calculateAverage(priceValues)
			avgOI := calculateAverage(oiValues)
			maxPrice := findMax(priceValues)
			minPrice := findMin(priceValues)

			fmt.Printf("\rWindow %d-%d of %d | Avg Price: %.2f | Max: %.2f | Min: %.2f | Avg OI: %.0f",
				windowStart+1, windowEnd, totalRecords, avgPrice, maxPrice, minPrice, avgOI)
		}

		// 等待并移动窗口
		time.Sleep(UPDATE_INTERVAL)
		windowStart += 50 // 每次移动50个点，加快滚动速度
	}
}

// Web服务器
func startWebServer() {
	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/chart", chartHandler)
	http.HandleFunc("/data", dataHandler)

	fmt.Printf("\n\nStarting web server at http://localhost%s\n", WEB_PORT)
	fmt.Println("Open your browser and visit the URL above to view the live chart")

	log.Fatal(http.ListenAndServe(WEB_PORT, nil))
}

// 主页处理器
func indexHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := `
<!DOCTYPE html>
<html>
<head>
    <title>JM2509 Live Chart</title>
    <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
    <style>
        body { 
            font-family: Arial, sans-serif; 
            margin: 20px; 
            background-color: #f5f5f5;
        }
        .container { 
            max-width: 1400px; 
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
            height: 600px;
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
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>JM2509 实时市场数据图表</h1>
            <p>价格和持仓量滚动显示</p>
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

        <div class="controls">
            <button onclick="toggleAutoUpdate()">暂停/继续更新</button>
            <button onclick="resetChart()">重置图表</button>
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
        let autoUpdate = true;
        let updateInterval;

        // 初始化图表
        function initChart() {
            const ctx = document.getElementById('myChart').getContext('2d');
            chart = new Chart(ctx, {
                type: 'line',
                data: {
                    labels: [],
                    datasets: [{
                        label: '价格',
                        data: [],
                        borderColor: 'rgb(75, 192, 192)',
                        backgroundColor: 'rgba(75, 192, 192, 0.1)',
                        tension: 0.1,
                        yAxisID: 'y'
                    }, {
                        label: '持仓量 (标准化)',
                        data: [],
                        borderColor: 'rgb(255, 99, 132)',
                        backgroundColor: 'rgba(255, 99, 132, 0.1)',
                        tension: 0.1,
                        yAxisID: 'y1'
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
                                text: '时间'
                            }
                        },
                        y: {
                            type: 'linear',
                            display: true,
                            position: 'left',
                            title: {
                                display: true,
                                text: '价格'
                            }
                        },
                        y1: {
                            type: 'linear',
                            display: true,
                            position: 'right',
                            title: {
                                display: true,
                                text: '持仓量'
                            },
                            grid: {
                                drawOnChartArea: false,
                            },
                        }
                    },
                    plugins: {
                        legend: {
                            display: true,
                            position: 'top'
                        },
                        title: {
                            display: true,
                            text: 'JM2509 实时数据'
                        }
                    }
                }
            });
        }

        // 更新图表数据
        function updateChart() {
            if (!autoUpdate) return;
            
            fetch('/data')
                .then(response => response.json())
                .then(data => {
                    if (data.error) {
                        document.getElementById('status').textContent = '错误: ' + data.error;
                        return;
                    }

                    // 更新图表数据
                    const labels = data.data.map(item => {
                        const date = new Date(item.time);
                        return date.toLocaleTimeString();
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
                        '最后更新: ' + new Date().toLocaleTimeString() + 
                        ' | 数据窗口: ' + data.window_info;
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
            document.getElementById('avgOI').textContent = Math.round(stats.avg_oi);
            document.getElementById('dataPoints').textContent = stats.data_points;
        }

        // 切换自动更新
        function toggleAutoUpdate() {
            autoUpdate = !autoUpdate;
            if (autoUpdate) {
                updateInterval = setInterval(updateChart, 2000);
                document.getElementById('status').textContent = '自动更新已启用';
            } else {
                clearInterval(updateInterval);
                document.getElementById('status').textContent = '自动更新已暂停';
            }
        }

        // 重置图表
        function resetChart() {
            if (chart) {
                chart.data.labels = [];
                chart.data.datasets[0].data = [];
                chart.data.datasets[1].data = [];
                chart.update();
            }
            updateChart();
        }

        // 页面加载完成后初始化
        window.onload = function() {
            initChart();
            updateChart();
            updateInterval = setInterval(updateChart, 2000);
        };
    </script>
</body>
</html>`

	t, err := template.New("index").Parse(tmpl)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = t.Execute(w, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// 图表处理器 (生成PNG图表)
func chartHandler(w http.ResponseWriter, r *http.Request) {
	dataMutex.RLock()
	data := currentData
	dataMutex.RUnlock()

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

	// 标准化持仓量数据到价格范围
	normalizedOI := normalizeToRange(oiValues, priceValues)

	// 创建图表
	graph := chart.Chart{
		Title: fmt.Sprintf("JM2509 - Price and Open Interest Chart (Window: %d-%d)",
			windowStart+1, windowStart+len(data)),
		TitleStyle: chart.Style{
			FontSize: 16,
		},
		Width:  1200,
		Height: 600,
		Background: chart.Style{
			Padding: chart.Box{
				Top:    50,
				Left:   50,
				Right:  50,
				Bottom: 50,
			},
		},
		XAxis: chart.XAxis{
			Name: "Time",
			Style: chart.Style{
				FontSize: 10,
			},
			ValueFormatter: chart.TimeValueFormatterWithFormat("15:04:05"),
		},
		YAxis: chart.YAxis{
			Name: "Price",
			Style: chart.Style{
				FontSize: 10,
			},
		},
		Series: []chart.Series{
			chart.TimeSeries{
				Name: "Price",
				Style: chart.Style{
					StrokeColor: drawing.ColorGreen,
					StrokeWidth: 2,
				},
				XValues: xValues,
				YValues: priceValues,
			},
			chart.TimeSeries{
				Name: "Open Interest (normalized)",
				Style: chart.Style{
					StrokeColor: drawing.ColorRed,
					StrokeWidth: 2,
				},
				XValues: xValues,
				YValues: normalizedOI,
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
func dataHandler(w http.ResponseWriter, r *http.Request) {
	dataMutex.RLock()
	data := currentData
	dataMutex.RUnlock()

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
		"avg_price":   calculateAverage(priceValues),
		"max_price":   findMax(priceValues),
		"min_price":   findMin(priceValues),
		"avg_oi":      calculateAverage(oiValues),
		"data_points": len(data),
	}

	windowInfo := fmt.Sprintf("%d-%d of %d", windowStart+1, windowStart+len(data), len(allData))

	response := map[string]interface{}{
		"data":        data,
		"stats":       stats,
		"window_info": windowInfo,
		"timestamp":   time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func normalizeToRange(source, target []float64) []float64 {
	if len(source) == 0 || len(target) == 0 {
		return source
	}

	sourceMin := findMin(source)
	sourceMax := findMax(source)
	targetMin := findMin(target)
	targetMax := findMax(target)

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

func findMax(data []float64) float64 {
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

func findMin(data []float64) float64 {
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

func calculateAverage(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	sum := 0.0
	for _, val := range data {
		sum += val
	}
	return sum / float64(len(data))
}
