package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

const (
	WINDOW_SIZE     = 200
	UPDATE_INTERVAL = 5 * time.Second
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

	// 初始化termui
	if err := termui.Init(); err != nil {
		log.Fatalf("failed to initialize termui: %v", err)
	}
	defer termui.Close()

	// 创建图表
	createChart(data)
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

func queryLatestMarketData(limit int) ([]MarketData, error) {
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
		FROM feature.jm 
		WHERE symbol = 'jm2509'
		ORDER BY time DESC 
		LIMIT %d
		FORMAT TabSeparated
	`, limit)

	result, err := executeQuery(query)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	data, err := parseTabSeparatedData(result)
	if err != nil {
		return nil, err
	}

	// 反转数据，使其按时间升序排列
	for i, j := 0, len(data)-1; i < j; i, j = i+1, j-1 {
		data[i], data[j] = data[j], data[i]
	}

	return data, nil
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

func createChart(allData []MarketData) {
	if len(allData) == 0 {
		log.Fatal("No data to display")
	}

	// 创建线图组件
	lineChart := widgets.NewPlot()
	lineChart.Title = "JM2509 - Price and Open Interest Chart (Scrolling Window)"
	lineChart.Data = make([][]float64, 2)
	lineChart.LineColors[0] = termui.ColorGreen // 价格线 - 绿色
	lineChart.LineColors[1] = termui.ColorRed   // 持仓量线 - 红色
	lineChart.AxesColor = termui.ColorWhite

	info := widgets.NewParagraph()
	info.Title = "Legend & Controls"
	info.Text = "Green Line: Price\nRed Line: Open Interest (normalized)\n\nPress 'q' to quit\nPress 'r' to refresh data\nLeft/Right: Manual scroll"

	stats := widgets.NewParagraph()
	stats.Title = "Statistics"

	// 设置初始布局
	updateLayout := func() {
		termWidth, termHeight := termui.TerminalDimensions()
		lineChart.SetRect(0, 0, termWidth, termHeight-20)
		info.SetRect(0, termHeight-20, termWidth/2, termHeight-10)
		stats.SetRect(termWidth/2, termHeight-20, termWidth, termHeight-10)
	}
	updateLayout()

	// 数据窗口索引
	windowStart := 0
	totalRecords := len(allData)

	// 更新图表数据的函数
	updateChart := func() {
		windowEnd := windowStart + WINDOW_SIZE
		if windowEnd > totalRecords {
			windowEnd = totalRecords
		}

		if windowStart >= totalRecords {
			windowStart = totalRecords - WINDOW_SIZE
			if windowStart < 0 {
				windowStart = 0
			}
			windowEnd = totalRecords
		}

		currentData := allData[windowStart:windowEnd]

		if len(currentData) < 2 {
			return
		}

		// 准备数据
		priceData := make([]float64, len(currentData))
		oiData := make([]float64, len(currentData))

		for i, record := range currentData {
			priceData[i] = float64(record.Price)
			oiData[i] = float64(record.OpenInterest)
		}

		// 标准化持仓量数据
		normalizedOI := normalizeData(oiData, priceData)

		// 更新图表数据
		lineChart.Data[0] = priceData
		lineChart.Data[1] = normalizedOI

		// 更新标题显示当前窗口信息
		lineChart.Title = fmt.Sprintf("JM2509 - Records %d-%d of %d (Window: %d points)",
			windowStart+1, windowEnd, totalRecords, len(currentData))

		// 更新统计信息
		avgPrice := calculateAverage(priceData)
		avgOI := calculateAverage(oiData)
		maxPrice := findMax(priceData)
		minPrice := findMin(priceData)

		var timeRange string
		if len(currentData) > 0 {
			timeRange = fmt.Sprintf("%s - %s",
				currentData[0].Time.Format("15:04:05"),
				currentData[len(currentData)-1].Time.Format("15:04:05"))
		}

		stats.Text = fmt.Sprintf("Time Range: %s\nAvg Price: %.2f\nMax Price: %.2f\nMin Price: %.2f\nAvg Open Interest: %.0f\nWindow: %d/%d",
			timeRange, avgPrice, maxPrice, minPrice, avgOI, windowStart/WINDOW_SIZE+1, (totalRecords+WINDOW_SIZE-1)/WINDOW_SIZE)
	}

	// 初始更新
	updateChart()
	termui.Render(lineChart, info, stats)

	// 创建定时器用于自动滚动
	ticker := time.NewTicker(UPDATE_INTERVAL)
	defer ticker.Stop()

	// 事件循环
	uiEvents := termui.PollEvents()
	for {
		select {
		case e := <-uiEvents:
			switch e.ID {
			case "q", "<C-c>":
				return
			case "r":
				// 刷新数据
				newData, err := queryMarketData()
				if err != nil {
					log.Printf("Failed to refresh data: %v", err)
				} else {
					allData = newData
					totalRecords = len(allData)
					windowStart = 0
					updateChart()
					termui.Clear()
					termui.Render(lineChart, info, stats)
				}
			case "<Resize>":
				updateLayout()
				termui.Clear()
				termui.Render(lineChart, info, stats)
			case "<Left>":
				// 向前滚动
				if windowStart > 0 {
					windowStart -= WINDOW_SIZE / 4
					if windowStart < 0 {
						windowStart = 0
					}
					updateChart()
					termui.Clear()
					termui.Render(lineChart, info, stats)
				}
			case "<Right>":
				// 向后滚动
				if windowStart+WINDOW_SIZE < totalRecords {
					windowStart += WINDOW_SIZE / 4
					updateChart()
					termui.Clear()
					termui.Render(lineChart, info, stats)
				}
			}
		case <-ticker.C:
			// 自动向前滚动
			if windowStart+WINDOW_SIZE < totalRecords {
				windowStart += 1
				updateChart()
				termui.Clear()
				termui.Render(lineChart, info, stats)
			}
		}
	}
}

// 标准化数据，将持仓量数据缩放到价格数据的范围内
func normalizeData(source, target []float64) []float64 {
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
