# Perfect Trading Bot

A Go-based trading bot that uses Polygon.io for market data and SignalStack for execution. It includes a live trading mode and a backtesting engine.

## Prerequisites

- Go 1.21 or higher
- Polygon.io API Key
- SignalStack Webhook URL (for live trading)

## Setup

1.  Clone the repository.
2.  Create a `.env` file in the root directory (see [Environment Variables](#environment-variables)).
3.  Install dependencies:

    ```bash
    go mod download
    ```

## Environment Variables

Create a `.env` file in the root directory with the following variables:

```env
# Required for both Live Trading and Backtesting
POLYGON_API_KEY=your_polygon_api_key

# Required for Live Trading
SIGNALSTACK_WEBHOOK_URL=your_signalstack_webhook_url

# Required Risk Configuration
ACCOUNT_SIZE=25000              # Used for position sizing & limits (must be > 0)
# Optional overrides (all capped to 1% of account size automatically)
MAX_DAILY_LOSS=250              # or set MAX_DAILY_LOSS_PCT=0.01
HARD_STOP_LOSS=125              # or set HARD_STOP_LOSS_PCT=0.5

# Optional for Backtesting (comma-separated list)
BACKTEST_TICKERS=AAPL,TSLA,AMD
BLACKLIST=RDDT,DJT,RKLB
```

## Running the Bot (Live Trading)

To start the bot in live trading mode:

```bash
go run main.go
```

The bot will:
1.  Connect to the Polygon.io WebSocket feed.
2.  Scan for potential trading candidates.
3.  Execute trades via SignalStack based on the strategy.
4.  Manage risk and track positions.

### Trading Rules & Limits
- Buying power is capped at the account size during regular hours and 1/16th of the account size in pre-market/after-hours.
- Max daily loss is automatically limited to 1% of the account size (TTP requirement). Hard stop defaults to half of that unless overridden.
- Maximum share size for any trade is 2500 shares.
- Pre-market and after-hours trades are allowed, but all open positions are force-closed before 3:50 PM ET and no new trades open again until after 3:50 PM ET.
- Trades cannot be less than 30 seconds in length
- Winning trades that are less than $0.10/share will not be counted towards your P&L. If you sell short 10 shares of a stock at $5.10, and buy to cover at $5.05, that trade's profits will not count toward your P&L, except for any commissions you occured taking that trade.
    - Losing trades that are less than $0.10/share WILL still count against your P&L.
- There are no costs for borrowing or locating shares for shorting with TTP
- The trading commission on Trade The Pool is 1/2 cent per share, with a minimum cost of $0.75 per filled order. For example:
    - Buying 50 shares of AAPL in one order will cost you $0.75 (the minimum fee).
    - Buying 200 shares of AAPL in one order will cost you $1.00 (based on the number of shares).
- During evaluation account, no single trade can account for more than 30% of your total profits to pass the evaluation. Once live funded, this rule no longer applies
    - Example: $250000 account, no single trade can exceed $450
    - Example: $100000 account, no single trade can exceed $1800
- If account ever reaches (AccountSize - 3(AccountSize*0.01)), then the account is closed, the eval is lost, and all trading should be stopped on that account. 
    - Example: $25000 account reaches $24250
    - Example: $100000 account reaches $97000
- If account ever reaches (AccountSize + (AccountSize * 0.06)), then the account is closed, the eval is WON!! You can stop trading on this account because a new funded account will be created. Your goal is to pass this evaluation in 20-30 days. 

## Backtesting

The bot includes a **realistic backtest engine** that simulates day-by-day trading with proper account balance tracking and buying power management. This engine processes each trading day sequentially, just like the live bot would operate, ensuring that:

- Account balance changes with each trade (wins increase balance, losses decrease it)
- Buying power is calculated realistically (account balance minus capital tied up in open positions)
- The bot can only enter trades when sufficient buying power is available
- Multiple tickers are processed simultaneously, with the bot selecting the best opportunities each day

### Running the Backtest

To run a backtest:

```bash
go run cmd/backtest/main.go [flags]
```

**Note:** By default, the backtest runs in simple mode (`-realistic=false`). However, the simple backtest engine is not yet implemented. To run a backtest, you must use `-realistic=true` to use the realistic day-by-day backtest engine.

### Flags

-   `-ticker`: Single ticker symbol to backtest (overrides `BACKTEST_TICKERS` env var). For multiple tickers, use `BACKTEST_TICKERS` env var.
-   `-days`: Number of days to look back (default: 30).
-   `-account`: Initial account size (default: 25000).
-   `-risk`: Risk percentage per trade (default: 0.005 for 0.5%). Note: actual risk is capped at 1% of account size.
-   `-eval`: Enable eval mode - limits single trade profit to 1.8% of account size (default: true).
-   `-realistic`: Use realistic backtest engine - day-by-day processing with account balance tracking (default: false). Set to `true` to enable.
-   `-runs`: Number of backtests to run simultaneously (default: 1). Useful for running multiple backtests concurrently.

### Examples

**Run realistic backtest for AAPL over the last 30 days:**

```bash
go run cmd/backtest/main.go -ticker AAPL -realistic=true
```

**Run realistic backtest for multiple tickers over the last 60 days with $50,000 account:**

```bash
# Set in .env file:
# BACKTEST_TICKERS=AAPL,TSLA,AMD

go run cmd/backtest/*.go -days 60 -account 50000 -realistic=true
```

**Run realistic backtest with eval mode disabled:**

```bash
go run cmd/backtest/*.go -ticker AAPL -realistic=true -eval=false
```

**Run simple backtest (ticker-by-ticker, no account balance tracking - not yet implemented):**

```bash
go run cmd/backtest/*.go -ticker AAPL -realistic=false
```

**Run multiple backtests simultaneously:**

```bash
go run cmd/backtest/*.go -ticker AAPL -realistic=true -runs 5
```

**Run realistic backtest using tickers defined in `.env`:**

```bash
go run cmd/backtest/*.go -realistic=true
```

### How the Realistic Backtest Works

The realistic backtest engine processes trading **day-by-day** (** All bar data must be pulled at the beginning of the test, loaded into state and then used. Pulling each ticker's data at the start of every day WILL get rate limited **), simulating exactly how the live bot would operate:

1. **Daily Processing:**
   - Each trading day is processed sequentially
   - Daily P&L resets at the start of each day
   - Account balance persists across days (wins/losses accumulate)

2. **Entry Signal Detection:**
   - Scans all provided tickers for entry signals throughout the day
   - Checks entry conditions minute-by-minute (VWAP extension, RSI filters, death candle pattern)
   - Only enters trades when sufficient buying power is available

3. **Buying Power Management:**
   - Buying power = Account balance - Capital tied up in open positions
   - For short positions, 50% margin requirement is used
   - Cannot enter new trades if buying power is insufficient

4. **Position Management:**
   - Tracks open positions with full strategy state (VWAP, ATR, RSI indicators)
   - Manages stops, targets, partial profits, trailing stops, and time decay
   - Closes positions at EOD (3:50 PM rule) or when exit conditions are met

5. **Account Balance Updates:**
   - Account balance increases with winning trades
   - Account balance decreases with losing trades
   - Future trade sizing is based on current account balance (not initial balance)

### Results

The backtester outputs:
- **Console output:** Summary statistics including total trades, win rate, total P&L, and final account balance
- **CSV file:** Detailed trade log saved to `cmd/backtest/results/backtest_YYYYMMDD_HHMMSS_runN_Nd_Npct.csv` with columns:
  - `Ticker`: Stock symbol
  - `EntryTime`: Entry timestamp
  - `ExitTime`: Exit timestamp
  - `Direction`: Trade direction (SHORT/LONG)
  - `EntryPrice`: Entry price
  - `ExitPrice`: Exit price
  - `Shares`: Number of shares traded
  - `Reason`: Exit reason (Stop Loss, Target, EOD, Time Decay, etc.)
  - `GrossPnL`: Profit/Loss before commissions
  - `Commission`: Trading commission
  - `NetPnL`: Profit/Loss after commissions and profit threshold rules

### Differences from Simple Backtest

The realistic backtest engine differs from a simple ticker-by-ticker backtest in several important ways:

| Feature | Realistic Engine | Simple Backtest |
|---------|------------------|-----------------|
| Processing | Day-by-day across all tickers | Ticker-by-ticker |
| Account Balance | Updates with each trade | Static (uses initial balance) |
| Buying Power | Calculates based on open positions | Uses full account size |
| Trade Selection | Bot picks best opportunity each day | Processes all tickers independently |
| Realism | Simulates actual bot behavior | Theoretical performance |

This makes the realistic backtest much more accurate for understanding how the bot would perform in live trading, especially when account balance changes affect future trade sizing and buying power availability.

## Project Structure

-   `cmd/backtest`: Entry point for the backtesting tool.
-   `pkg/`: Core logic packages.
    -   `execution`: Handles trade execution (SignalStack).
    -   `feed`: Manages data feeds (Polygon.io).
    -   `risk`: Risk management logic.
    -   `scanner`: Scans for trading candidates.
    -   `strategy`: Implements trading strategies (VWAP, ATR, etc.).
-   `main.go`: Entry point for the live trading bot.