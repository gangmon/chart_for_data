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
)

const (
	WINDOW_SIZE     = 200
	UPDATE_INTERVAL = 2 * time.Second
	CHART_HEIGHT    = 20
	CHART_WIDTH     = 100
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
	fmt.Println("Starting chart display... Press Ctrl+C to exit")
	time.Sleep(2 * time.Second)

	// 创建图表
	createASCIIChart(data)
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

func createASCIIChart(allData []MarketData) {
	windowStart := 0
	totalRecords := len(allData)

	for {
		// 清屏
		fmt.Print("\033[2J\033[H")

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

		currentData := allData[windowStart:windowEnd]

		if len(currentData) < 2 {
			windowStart++
			continue
		}

		// 准备数据
		priceData := make([]float64, len(currentData))
		oiData := make([]float64, len(currentData))

		for i, record := range currentData {
			priceData[i] = float64(record.Price)
			oiData[i] = float64(record.OpenInterest)
		}

		// 标准化数据
		normalizedPrice := normalizeToRange(priceData, 0, CHART_HEIGHT-1)
		normalizedOI := normalizeToRange(oiData, 0, CHART_HEIGHT-1)

		// 绘制图表
		drawChart(normalizedPrice, normalizedOI, currentData)

		// 显示统计信息
		showStats(priceData, oiData, currentData, windowStart, windowEnd, totalRecords)

		// 等待并移动窗口
		time.Sleep(UPDATE_INTERVAL)
		windowStart += 5 // 每次移动5个点
	}
}

func normalizeToRange(data []float64, min, max int) []int {
	if len(data) == 0 {
		return []int{}
	}

	dataMin := findMin(data)
	dataMax := findMax(data)

	if dataMax == dataMin {
		// 如果所有值相同，返回中间值
		mid := (min + max) / 2
		result := make([]int, len(data))
		for i := range result {
			result[i] = mid
		}
		return result
	}

	result := make([]int, len(data))
	for i, val := range data {
		normalized := float64(min) + (val-dataMin)*(float64(max-min))/(dataMax-dataMin)
		result[i] = int(normalized)
	}

	return result
}

func drawChart(priceData, oiData []int, currentData []MarketData) {
	// 创建图表网格
	chart := make([][]rune, CHART_HEIGHT)
	for i := range chart {
		chart[i] = make([]rune, CHART_WIDTH)
		for j := range chart[i] {
			chart[i][j] = ' '
		}
	}

	// 绘制数据点
	dataLen := len(priceData)
	if dataLen > CHART_WIDTH {
		dataLen = CHART_WIDTH
	}

	for i := 0; i < dataLen; i++ {
		x := i * CHART_WIDTH / len(priceData)
		if x >= CHART_WIDTH {
			x = CHART_WIDTH - 1
		}

		// 绘制价格线 (绿色 - 用 * 表示)
		priceY := CHART_HEIGHT - 1 - priceData[i]
		if priceY >= 0 && priceY < CHART_HEIGHT {
			chart[priceY][x] = '*'
		}

		// 绘制持仓量线 (红色 - 用 # 表示)
		oiY := CHART_HEIGHT - 1 - oiData[i]
		if oiY >= 0 && oiY < CHART_HEIGHT {
			if chart[oiY][x] == '*' {
				chart[oiY][x] = '@' // 重叠时用 @ 表示
			} else {
				chart[oiY][x] = '#'
			}
		}
	}

	// 打印标题
	fmt.Printf("JM2509 - Price and Open Interest Chart (Window: %d points)\n", len(currentData))
	fmt.Println("Legend: * = Price, # = Open Interest, @ = Both")
	fmt.Println(strings.Repeat("=", CHART_WIDTH+10))

	// 打印图表
	for i := 0; i < CHART_HEIGHT; i++ {
		fmt.Printf("%2d |", CHART_HEIGHT-i-1)
		for j := 0; j < CHART_WIDTH; j++ {
			fmt.Printf("%c", chart[i][j])
		}
		fmt.Println("|")
	}

	// 打印底部边框
	fmt.Print("   +")
	fmt.Print(strings.Repeat("-", CHART_WIDTH))
	fmt.Println("+")

	// 打印时间轴
	if len(currentData) > 0 {
		fmt.Printf("   Time: %s -> %s\n",
			currentData[0].Time.Format("15:04:05"),
			currentData[len(currentData)-1].Time.Format("15:04:05"))
	}
}

func showStats(priceData, oiData []float64, currentData []MarketData, windowStart, windowEnd, totalRecords int) {
	avgPrice := calculateAverage(priceData)
	avgOI := calculateAverage(oiData)
	maxPrice := findMax(priceData)
	minPrice := findMin(priceData)

	fmt.Println(strings.Repeat("=", CHART_WIDTH+10))
	fmt.Printf("Statistics - Records %d-%d of %d\n", windowStart+1, windowEnd, totalRecords)
	fmt.Printf("Avg Price: %.2f | Max Price: %.2f | Min Price: %.2f\n", avgPrice, maxPrice, minPrice)
	fmt.Printf("Avg Open Interest: %.0f | Data Points: %d\n", avgOI, len(currentData))
	fmt.Printf("Window: %d/%d\n", windowStart/WINDOW_SIZE+1, (totalRecords+WINDOW_SIZE-1)/WINDOW_SIZE)
	fmt.Println(strings.Repeat("=", CHART_WIDTH+10))
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
