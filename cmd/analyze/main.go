package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/perfect-nt-bot/pkg/strategy"
)

func main() {
	// Parse command-line flags
	csvDirFlag := flag.String("csv-dir", "cmd/backtest/results", "Directory containing CSV backtest results")
	outputFlag := flag.String("output", "", "Output file path (JSON or HTML, default: stdout)")
	formatFlag := flag.String("format", "json", "Output format: json or html")
	flag.Parse()

	fmt.Println("Analyzing backtest results...")
	fmt.Printf("CSV Directory: %s\n", *csvDirFlag)

	// Load all CSV files
	files, err := filepath.Glob(filepath.Join(*csvDirFlag, "*.csv"))
	if err != nil {
		log.Fatalf("Failed to list CSV files: %v", err)
	}

	if len(files) == 0 {
		log.Fatalf("No CSV files found in %s", *csvDirFlag)
	}

	// Aggregate statistics
	stats := NewAggregateStats()

	for _, file := range files {
		trades, err := loadTradesFromCSV(file)
		if err != nil {
			fmt.Printf("Warning: Failed to load %s: %v\n", file, err)
			continue
		}

		for _, trade := range trades {
			stats.RecordTrade(trade)
		}
	}

	// Generate report
	report := stats.GenerateReport()

	// Output report
	if *outputFlag != "" {
		if *formatFlag == "html" {
			if err := exportHTML(report, *outputFlag); err != nil {
				log.Fatalf("Failed to export HTML: %v", err)
			}
		} else {
			if err := exportJSON(report, *outputFlag); err != nil {
				log.Fatalf("Failed to export JSON: %v", err)
			}
		}
		fmt.Printf("Report exported to: %s\n", *outputFlag)
	} else {
		// Print to stdout
		printReport(report)
	}
}

// TradeData represents a trade from CSV
type TradeData struct {
	Ticker     string
	EntryTime  time.Time
	ExitTime   time.Time
	Direction  string
	EntryPrice float64
	ExitPrice  float64
	Shares     int
	Reason     strategy.ExitReason
	NetPnL     float64
}

// AggregateStats aggregates statistics across multiple backtest runs
type AggregateStats struct {
	TotalTrades      int
	TotalWins        int
	TotalLosses      int
	TotalPnL         float64
	WinRateByHour    map[int]struct{ Wins, Losses, Total int }
	WinRateByReason  map[string]struct{ Wins, Losses, Total int }
	WinRateByDirection map[string]struct{ Wins, Losses, Total int }
	AverageWin       float64
	AverageLoss       float64
	BestTrade        *TradeData
	WorstTrade       *TradeData
}

// NewAggregateStats creates a new aggregate stats tracker
func NewAggregateStats() *AggregateStats {
	return &AggregateStats{
		WinRateByHour:     make(map[int]struct{ Wins, Losses, Total int }),
		WinRateByReason:   make(map[string]struct{ Wins, Losses, Total int }),
		WinRateByDirection: make(map[string]struct{ Wins, Losses, Total int }),
	}
}

// RecordTrade records a trade for statistics
func (as *AggregateStats) RecordTrade(trade *TradeData) {
	as.TotalTrades++
	as.TotalPnL += trade.NetPnL

	isWin := trade.NetPnL > 0
	if isWin {
		as.TotalWins++
		if as.AverageWin == 0 {
			as.AverageWin = trade.NetPnL
		} else {
			as.AverageWin = (as.AverageWin*float64(as.TotalWins-1) + trade.NetPnL) / float64(as.TotalWins)
		}
		if as.BestTrade == nil || trade.NetPnL > as.BestTrade.NetPnL {
			as.BestTrade = trade
		}
	} else {
		as.TotalLosses++
		if as.AverageLoss == 0 {
			as.AverageLoss = trade.NetPnL
		} else {
			as.AverageLoss = (as.AverageLoss*float64(as.TotalLosses-1) + trade.NetPnL) / float64(as.TotalLosses)
		}
		if as.WorstTrade == nil || trade.NetPnL < as.WorstTrade.NetPnL {
			as.WorstTrade = trade
		}
	}

	// Record by hour
	hour := trade.EntryTime.Hour()
	hourStat := as.WinRateByHour[hour]
	hourStat.Total++
	if isWin {
		hourStat.Wins++
	} else {
		hourStat.Losses++
	}
	as.WinRateByHour[hour] = hourStat

	// Record by reason
	reasonStat := as.WinRateByReason[string(trade.Reason)]
	reasonStat.Total++
	if isWin {
		reasonStat.Wins++
	} else {
		reasonStat.Losses++
	}
	as.WinRateByReason[string(trade.Reason)] = reasonStat

	// Record by direction
	dirStat := as.WinRateByDirection[trade.Direction]
	dirStat.Total++
	if isWin {
		dirStat.Wins++
	} else {
		dirStat.Losses++
	}
	as.WinRateByDirection[trade.Direction] = dirStat
}

// Report represents the analysis report
type Report struct {
	TotalTrades      int
	TotalWins        int
	TotalLosses      int
	WinRate          float64
	TotalPnL         float64
	AverageWin        float64
	AverageLoss       float64
	WinRateByHour     map[int]float64
	WinRateByReason   map[string]float64
	WinRateByDirection map[string]float64
	BestTrade         *TradeData
	WorstTrade        *TradeData
}

// GenerateReport generates a report from aggregated stats
func (as *AggregateStats) GenerateReport() *Report {
	report := &Report{
		TotalTrades:       as.TotalTrades,
		TotalWins:         as.TotalWins,
		TotalLosses:       as.TotalLosses,
		TotalPnL:          as.TotalPnL,
		AverageWin:        as.AverageWin,
		AverageLoss:       as.AverageLoss,
		WinRateByHour:     make(map[int]float64),
		WinRateByReason:   make(map[string]float64),
		WinRateByDirection: make(map[string]float64),
		BestTrade:         as.BestTrade,
		WorstTrade:        as.WorstTrade,
	}

	if as.TotalTrades > 0 {
		report.WinRate = float64(as.TotalWins) / float64(as.TotalTrades) * 100
	}

	// Calculate win rates
	for hour, stat := range as.WinRateByHour {
		if stat.Total > 0 {
			report.WinRateByHour[hour] = float64(stat.Wins) / float64(stat.Total) * 100
		}
	}

	for reason, stat := range as.WinRateByReason {
		if stat.Total > 0 {
			report.WinRateByReason[reason] = float64(stat.Wins) / float64(stat.Total) * 100
		}
	}

	for direction, stat := range as.WinRateByDirection {
		if stat.Total > 0 {
			report.WinRateByDirection[direction] = float64(stat.Wins) / float64(stat.Total) * 100
		}
	}

	return report
}

// loadTradesFromCSV loads trades from a CSV file
func loadTradesFromCSV(filepath string) ([]*TradeData, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("CSV file has no data rows")
	}

	trades := make([]*TradeData, 0, len(records)-1)
	for i := 1; i < len(records); i++ {
		record := records[i]
		if len(record) < 11 {
			continue
		}

		entryTime, err := time.Parse(time.RFC3339, record[1])
		if err != nil {
			continue
		}
		exitTime, err := time.Parse(time.RFC3339, record[2])
		if err != nil {
			continue
		}

		entryPrice, _ := strconv.ParseFloat(record[4], 64)
		exitPrice, _ := strconv.ParseFloat(record[5], 64)
		shares, _ := strconv.Atoi(record[6])
		netPnL, _ := strconv.ParseFloat(record[10], 64)

		trade := &TradeData{
			Ticker:     record[0],
			EntryTime:  entryTime,
			ExitTime:   exitTime,
			Direction:  record[3],
			EntryPrice: entryPrice,
			ExitPrice:  exitPrice,
			Shares:     shares,
			Reason:     strategy.ExitReason(record[7]),
			NetPnL:     netPnL,
		}
		trades = append(trades, trade)
	}

	return trades, nil
}

// printReport prints the report to stdout
func printReport(report *Report) {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("BACKTEST ANALYSIS REPORT")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("Total Trades: %d\n", report.TotalTrades)
	fmt.Printf("Wins: %d, Losses: %d\n", report.TotalWins, report.TotalLosses)
	fmt.Printf("Win Rate: %.2f%%\n", report.WinRate)
	fmt.Printf("Total P&L: $%.2f\n", report.TotalPnL)
	fmt.Printf("Average Win: $%.2f\n", report.AverageWin)
	fmt.Printf("Average Loss: $%.2f\n", report.AverageLoss)

	fmt.Println("\nWin Rate by Hour:")
	hours := make([]int, 0, len(report.WinRateByHour))
	for hour := range report.WinRateByHour {
		hours = append(hours, hour)
	}
	sort.Ints(hours)
	for _, hour := range hours {
		fmt.Printf("  %d:00 - %.2f%%\n", hour, report.WinRateByHour[hour])
	}

	fmt.Println("\nWin Rate by Exit Reason:")
	for reason, winRate := range report.WinRateByReason {
		fmt.Printf("  %s - %.2f%%\n", reason, winRate)
	}

	fmt.Println("\nWin Rate by Direction:")
	for direction, winRate := range report.WinRateByDirection {
		fmt.Printf("  %s - %.2f%%\n", direction, winRate)
	}

	if report.BestTrade != nil {
		fmt.Printf("\nBest Trade: %s %s @ $%.2f, P&L: $%.2f\n",
			report.BestTrade.Ticker, report.BestTrade.Direction, report.BestTrade.EntryPrice, report.BestTrade.NetPnL)
	}
	if report.WorstTrade != nil {
		fmt.Printf("Worst Trade: %s %s @ $%.2f, P&L: $%.2f\n",
			report.WorstTrade.Ticker, report.WorstTrade.Direction, report.WorstTrade.EntryPrice, report.WorstTrade.NetPnL)
	}
}

// exportJSON exports the report as JSON
func exportJSON(report *Report, filepath string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath, data, 0644)
}

// exportHTML exports the report as HTML
func exportHTML(report *Report, filepath string) error {
	var html strings.Builder
	html.WriteString("<!DOCTYPE html>\n<html><head><title>Backtest Analysis</title></head><body>\n")
	html.WriteString("<h1>Backtest Analysis Report</h1>\n")
	html.WriteString(fmt.Sprintf("<p>Total Trades: %d</p>\n", report.TotalTrades))
	html.WriteString(fmt.Sprintf("<p>Win Rate: %.2f%%</p>\n", report.WinRate))
	html.WriteString(fmt.Sprintf("<p>Total P&L: $%.2f</p>\n", report.TotalPnL))
	html.WriteString("</body></html>\n")
	return os.WriteFile(filepath, []byte(html.String()), 0644)
}

