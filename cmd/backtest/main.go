package main

import (
	"flag"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/perfect-nt-bot/pkg/config"
	"github.com/perfect-nt-bot/pkg/feed"
	"github.com/perfect-nt-bot/pkg/scanner"
)

func main() {
	// Parse command-line flags
	tickerFlag := flag.String("ticker", "", "Single ticker symbol to backtest")
	daysFlag := flag.Int("days", 30, "Number of days to look back")
	accountFlag := flag.Float64("account", 25000, "Initial account size")
	riskFlag := flag.Float64("risk", 0.005, "Risk percentage per trade (default: 0.005 for 0.5%)")
	evalFlag := flag.Bool("eval", true, "Enable eval mode - limits single trade profit to 1.8% of account size")
	realisticFlag := flag.Bool("realistic", false, "Use realistic backtest engine (day-by-day processing)")
	runsFlag := flag.Int("runs", 1, "Number of backtests to run simultaneously (default: 1)")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Validate config
	if err := cfg.Validate(false); err != nil {
		log.Fatalf("Config validation failed: %v", err)
	}

	// Override account size from flag
	if *accountFlag != 25000 {
		cfg.AccountSize = *accountFlag
		cfg.ProfitTarget = cfg.AccountSize + (cfg.AccountSize * 0.06)
		cfg.AccountCloseLimit = cfg.AccountSize - (3 * (cfg.AccountSize * 0.01))
		cfg.MaxDailyLossLimit = cfg.AccountSize * 0.01  // 1% cap
		cfg.HardStopLossLimit = cfg.AccountSize * 0.005 // 0.5% cap
	}

	// Get ticker list
	var tickers []string
	if *tickerFlag != "" {
		tickers = []string{*tickerFlag}
	} else if len(cfg.BacktestTickers) > 0 {
		tickers = cfg.BacktestTickers
	} else {
		log.Fatal("No tickers specified. Use -ticker flag or set BACKTEST_TICKERS in .env")
	}

	fmt.Printf("Starting backtest...\n")
	fmt.Printf("Tickers: %v\n", tickers)
	fmt.Printf("Days: %d\n", *daysFlag)
	fmt.Printf("Account Size: $%.2f\n", cfg.AccountSize)
	fmt.Printf("Risk per trade: %.2f%%\n", *riskFlag*100)
	fmt.Printf("Eval mode: %v\n", *evalFlag)
	fmt.Printf("Realistic engine: %v\n", *realisticFlag)
	fmt.Printf("Number of runs: %d\n", *runsFlag)
	fmt.Printf("Profit Target: $%.2f\n", cfg.ProfitTarget)
	fmt.Printf("Account Close Limit: $%.2f\n", cfg.AccountCloseLimit)
	fmt.Println()

	// Create Polygon feed
	polygonFeed := feed.NewPolygonFeed(cfg.PolygonAPIKey)

	// Create scanner
	scanner := scanner.NewScanner(cfg)

	// Get ET timezone
	location, err := config.GetLocation()
	if err != nil {
		log.Fatalf("Failed to load timezone: %v", err)
	}

	if *realisticFlag {
		// Run realistic day-by-day backtest
		if err := runRealisticBacktest(polygonFeed, scanner, tickers, *daysFlag, cfg, *riskFlag, *evalFlag, location, *runsFlag); err != nil {
			log.Fatalf("Backtest failed: %v", err)
		}
	} else {
		// Run simple ticker-by-ticker backtest
		if err := runSimpleBacktest(polygonFeed, scanner, tickers, *daysFlag, cfg, *riskFlag, *evalFlag, location); err != nil {
			log.Fatalf("Backtest failed: %v", err)
		}
	}
}

// runSimpleBacktest runs a simple ticker-by-ticker backtest
func runSimpleBacktest(
	polygonFeed *feed.PolygonFeed,
	scanner *scanner.Scanner,
	tickers []string,
	days int,
	cfg *config.Config,
	riskPct float64,
	evalMode bool,
	location *time.Location,
) error {
	fmt.Println("Running simple backtest (ticker-by-ticker)...")
	// TODO: Implement simple backtest
	return fmt.Errorf("simple backtest not yet implemented")
}

// runRealisticBacktest runs a realistic day-by-day backtest
func runRealisticBacktest(
	polygonFeed *feed.PolygonFeed,
	scanner *scanner.Scanner,
	tickers []string,
	days int,
	cfg *config.Config,
	riskPct float64,
	evalMode bool,
	location *time.Location,
	runs int,
) error {
	fmt.Println("Running realistic backtest (day-by-day)...")

	// Fetch all historical data upfront once (per README requirement)
	fmt.Println("Fetching historical data...")
	barsByDate := make(map[time.Time]map[string][]feed.Bar)

	for _, ticker := range tickers {
		fmt.Printf("  Fetching %s...\n", ticker)
		tickerBars, err := polygonFeed.GetDaysOfBars(ticker, days)
		if err != nil {
			return fmt.Errorf("failed to fetch bars for %s: %v", ticker, err)
		}

		// Merge into barsByDate
		for date, bars := range tickerBars {
			if barsByDate[date] == nil {
				barsByDate[date] = make(map[string][]feed.Bar)
			}
			barsByDate[date][ticker] = bars
		}
	}

	fmt.Printf("Fetched data for %d trading days\n", len(barsByDate))
	fmt.Printf("Running %d backtest(s) simultaneously...\n", runs)

	// Run multiple backtests concurrently
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstError error
	var allRunStats []RunStats

	for i := 1; i <= runs; i++ {
		wg.Add(1)
		go func(runNum int) {
			defer wg.Done()

			fmt.Printf("\n[Run %d/%d] Starting backtest...\n", runNum, runs)

			// Create a new engine for this run
			engine := NewRealisticBacktestEngine(cfg, scanner, riskPct, evalMode, location)
			engine.runNumber = runNum

			// Deep copy barsByDate to avoid race conditions
			// Each engine needs its own copy of the data
			copiedBarsByDate := make(map[time.Time]map[string][]feed.Bar)
			for date, tickerMap := range barsByDate {
				copiedTickerMap := make(map[string][]feed.Bar)
				for ticker, bars := range tickerMap {
					// Copy the bars slice
					copiedBars := make([]feed.Bar, len(bars))
					copy(copiedBars, bars)
					copiedTickerMap[ticker] = copiedBars
				}
				copiedBarsByDate[date] = copiedTickerMap
			}

			if err := engine.Run(copiedBarsByDate); err != nil {
				mu.Lock()
				if firstError == nil {
					firstError = fmt.Errorf("run %d failed: %v", runNum, err)
				}
				mu.Unlock()
				return
			}

			// Collect statistics from this run
			stats := engine.GetRunStats()
			mu.Lock()
			allRunStats = append(allRunStats, stats)
			mu.Unlock()

			fmt.Printf("[Run %d/%d] Completed successfully\n", runNum, runs)
		}(i)
	}

	// Wait for all backtests to complete
	wg.Wait()

	if firstError != nil {
		return firstError
	}

	// Print combined statistics if we have multiple runs
	if runs > 1 && len(allRunStats) > 0 {
		// Sort stats by run number for consistent output
		sort.Slice(allRunStats, func(i, j int) bool {
			return allRunStats[i].RunNumber < allRunStats[j].RunNumber
		})
		printCombinedStats(allRunStats, cfg.ProfitTarget)
	}

	fmt.Printf("\nAll %d backtest(s) completed successfully!\n", runs)
	return nil
}

// printCombinedStats prints combined statistics across all runs
func printCombinedStats(allRunStats []RunStats, profitTarget float64) {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("=== COMBINED MULTI-RUN STATISTICS ===")
	fmt.Println(strings.Repeat("=", 60))

	// Calculate combined win rate
	totalTrades := 0
	totalWins := 0
	for _, stats := range allRunStats {
		totalTrades += stats.TotalTrades
		totalWins += stats.Wins
	}

	var combinedWinRate float64
	if totalTrades > 0 {
		combinedWinRate = float64(totalWins) / float64(totalTrades) * 100
	}

	fmt.Printf("Combined Win Rate: %.2f%% (%d wins / %d total trades)\n",
		combinedWinRate, totalWins, totalTrades)

	// Count runs that reached or reached at least 75% of profit target
	runsReachedTarget := make([]int, 0)
	runsReached75Percent := make([]int, 0)

	// Calculate 75% threshold for display
	var accountSize float64
	if len(allRunStats) > 0 {
		accountSize = allRunStats[0].AccountSize
	}
	profitNeeded := profitTarget - accountSize
	seventyFivePercentThreshold := accountSize + (profitNeeded * 0.75)

	for _, stats := range allRunStats {
		if stats.ReachedTarget {
			runsReachedTarget = append(runsReachedTarget, stats.RunNumber)
		} else if stats.Reached75Percent {
			runsReached75Percent = append(runsReached75Percent, stats.RunNumber)
		}
	}

	totalReachedOr75Percent := len(runsReachedTarget) + len(runsReached75Percent)

	fmt.Printf("\nProfit Target: $%.2f (Account: $%.2f, Profit Needed: $%.2f)\n",
		profitTarget, accountSize, profitNeeded)
	fmt.Printf("75%% Threshold: $%.2f (at least $%.2f profit)\n",
		seventyFivePercentThreshold, profitNeeded*0.75)
	fmt.Printf("\nRuns that reached profit target: %d\n", len(runsReachedTarget))
	if len(runsReachedTarget) > 0 {
		fmt.Printf("  Runs: %v\n", runsReachedTarget)
	}

	fmt.Printf("Runs that reached at least 75%% of profit target: %d\n", len(runsReached75Percent))
	if len(runsReached75Percent) > 0 {
		fmt.Printf("  Runs: %v\n", runsReached75Percent)
	}

	fmt.Printf("\nTotal runs that reached or reached at least 75%% of profit target: %d\n",
		totalReachedOr75Percent)
	if totalReachedOr75Percent > 0 {
		allTargetRuns := append(runsReachedTarget, runsReached75Percent...)
		sort.Ints(allTargetRuns)
		fmt.Printf("  All qualifying runs: %v\n", allTargetRuns)
	}

	fmt.Println(strings.Repeat("=", 60))
}
