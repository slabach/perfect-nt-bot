# Backtest Analysis & Improvement Recommendations

## Executive Summary

**Current Performance:**
- Total Net P&L: **$1,287.88** (Target: $1,500.00 - **$212.12 short of goal**)
- Win Rate: **35.7%** (15 wins / 27 losses)
- Total Trades: **42** over 23 days (~1.8 trades/day)
- Overall Win/Loss Ratio: 3.70 (good when wins occur, but win rate too low)

**Profit Target:** Account needs to reach $26,500 from $25,000 = **$1,500 profit (6% gain)**

---

## Critical Issues Identified

### 1. **EOD Exits Are Killing Performance** ⚠️ HIGH PRIORITY
- **47.6% of all trades (20/42) close at EOD**
- **EOD Win Rate: Only 10.0%** (2 wins / 18 losses)
- **EOD Total P&L: -$373.69** (largest drag on performance)
- **Average EOD Loss: -$18.68 per trade**

**Root Cause:** Positions held until market close that should have exited earlier
- Current logic exits losing positions at 3:00 PM (50 min before EOD)
- However, many positions are still held until EOD and losing money
- Only 10% of EOD exits are profitable

**Recommendations:**
1. **Exit ALL losing positions by 2:30 PM** (earlier cutoff)
2. **Exit ALL positions by 3:00 PM** if not profitable (don't wait for 3:50 PM)
3. **Only hold until EOD if position is profitable by 3:00 PM** (let winners run)
4. Consider holding profitable positions overnight (if allowed by rules)

### 2. **Win Rate Too Low** ⚠️ HIGH PRIORITY
- **35.7% win rate** is far below acceptable (target: 50%+)
- **Win/Loss ratio of 3.70 is good**, but low win rate prevents it from helping
- Only 13 trades (31%) hit Target 1 (which has 92.3% win rate)

**Root Cause:** Entry criteria may be too relaxed or timing is poor

**Recommendations:**
1. **Tighten entry filters:**
   - Increase VWAP extension threshold from 0.5x to 0.7x ATR (require stronger extension)
   - Increase RSI threshold from 55 to 60 (require more overbought condition)
   - Require death candle pattern (currently allows entries without pattern)
   - Increase volume requirement from 0.5x to 1.0x average volume
2. **Improve entry timing:**
   - Avoid entries within first 30 minutes of market open (9:30-10:00 AM)
   - Avoid entries after 1:00 PM (currently 2:00 PM cutoff)
3. **Add momentum confirmation:**
   - Require price moving away from VWAP (not just extended above it)
   - Require RSI declining from peak (momentum reversal signal)

### 3. **Trailing Stops Are Losing Money** ⚠️ MEDIUM PRIORITY
- **4 trades** exited via trailing stop
- **0% win rate** (all 4 lost money)
- **Total P&L: -$128.50**
- Average loss: -$32.12 per trade

**Root Cause:** Trailing stop logic may be activating incorrectly or too early

**Analysis:**
- Trailing stops activate after Target 1 is filled
- Trailing stop offset: $0.10 per share
- Issue: Trailing stops may be triggering on small pullbacks

**Recommendations:**
1. **Disable trailing stops entirely** for now (test without them)
2. If keeping, **increase trailing stop offset** from $0.10 to $0.15-$0.20
3. **Only activate trailing stop** if position is > $0.20/share profitable (not just after Target 1)
4. Use **ATR-based trailing stop** instead of fixed $0.10 offset

### 4. **Stop Losses Too Large** ⚠️ MEDIUM PRIORITY
- **2 trades** hit stop loss
- **Average loss: -$201.89** per trade
- Largest stop loss: -$259.07 (BITF trade)

**Root Cause:** Stop loss distance (1.2x ATR) may be too wide for volatile stocks

**Recommendations:**
1. **Tighten stop loss** from 1.2x to 1.0x ATR
2. **Add position sizing based on ATR** - smaller positions for high volatility stocks
3. **Dynamic stop loss** - tighter stops for stocks with lower ATR, wider for higher ATR

### 5. **Commission Impact Too High** ⚠️ MEDIUM PRIORITY
- **Commission: $206.63** (16.04% of net P&L)
- This is eating into profits significantly

**Root Cause:** Too many small trades with minimum commission ($0.75 per order)

**Recommendations:**
1. **Increase minimum position size** to reduce commission impact
2. **Filter out low-price stocks** (< $5) that require more shares to risk same amount
3. **Focus on higher-value trades** where commission is lower % of profit

### 6. **Not Enough Trades** ⚠️ LOW PRIORITY
- Only **42 trades in 23 days** (~1.8 trades/day)
- More opportunities might help reach profit target

**Recommendations:**
1. **Slightly relax entry criteria** AFTER fixing win rate issue
2. **Increase max concurrent positions** from 3 to 4-5
3. **Scan more tickers** (if scanner is limiting tickers)

---

## Performance by Exit Reason

| Exit Reason | Count | Win Rate | Total P&L | Avg P&L | Status |
|------------|-------|----------|-----------|---------|--------|
| **Target 1** | 13 | **92.3%** | **+$2,288.06** | +$176.00 | ✅ Excellent |
| **End of Day** | 20 | **10.0%** | **-$373.69** | -$18.68 | ❌ Critical Issue |
| **Trailing Stop** | 4 | **0.0%** | **-$128.50** | -$32.12 | ❌ Losing Money |
| **Stop Loss** | 2 | **0.0%** | **-$403.79** | -$201.89 | ⚠️ Too Large |
| **Time Decay** | 2 | **50.0%** | **+$136.99** | +$68.50 | ✅ Good |
| **Max Daily Loss** | 1 | **0.0%** | **-$231.19** | -$231.19 | ⚠️ Risk Management Working |

**Key Insight:** If we could eliminate EOD losses and improve win rate to hit Target 1 more often, we'd easily exceed profit target.

---

## Performance by Entry Hour

| Hour | Trades | Avg P&L | Total P&L | Status |
|------|--------|---------|-----------|--------|
| 9:00 AM | 6 | +$32.71 | +$196.24 | ✅ Good |
| 10:00 AM | 10 | +$58.56 | +$585.63 | ✅✅ Best |
| 11:00 AM | 14 | +$28.20 | +$394.76 | ✅ Good |
| 12:00 PM | 7 | +$9.60 | +$67.19 | ⚠️ Weak |
| 13:00 PM | 5 | +$8.81 | +$44.06 | ⚠️ Weak |

**Key Insight:** 
- **10:00 AM entries are best** (+$58.56 avg)
- **Afternoon entries (12:00 PM+) underperform** significantly
- Current 2:00 PM cutoff is good, but consider moving to 1:00 PM

---

## Top Losing Trades

1. **BITF** - Stop Loss: -$259.07 (4.74 → 4.90, 1543 shares)
2. **CMBM** - Max Daily Loss: -$231.19 (4.75 → 5.68, 247 shares)
3. **TSLA** - Stop Loss: -$144.72 (463.17 → 465.44, 63 shares)
4. **QUBT** - Trailing Stop: -$67.87 (15.42 → 15.53, 581 shares)
5. **WULF** - End of Day: -$54.07 (16.02 → 16.13, 449 shares)

**Pattern:** Large losses from:
- Wide stops on volatile stocks (BITF, CMBM)
- Trailing stops cutting winners short (QUBT)
- EOD exits on losing positions (WULF)

---

## Top Winning Trades

1. **MRNA** - Target 1: +$443.31 (26.96 → 25.29, 266 shares) ✅
2. **TSLA** - Target 1: +$332.90 (456.10 → 440.90, 22 shares) ✅
3. **PLTR** - Target 1: +$280.45 (196.34 → 190.34, 47 shares) ✅
4. **VCIG** - Target 1: +$244.71 (3.17 → 2.66, 490 shares) ✅
5. **IONQ** - Target 1: +$222.80 (65.26 → 63.28, 113 shares) ✅

**Pattern:** All winners hit Target 1. Strategy works when entries are good!

---

## Recommended Action Plan

### Phase 1: Quick Wins (Implement First)
1. **Fix EOD Exit Logic** (Priority #1)
   - Exit ALL losing positions by 2:30 PM
   - Exit ALL positions by 3:00 PM if not profitable
   - Only hold until 3:50 PM if profitable by 3:00 PM

2. **Disable Trailing Stops** (Priority #2)
   - Remove trailing stop exits temporarily
   - Let Target 1/2 handle profit taking

3. **Tighten Stop Loss** (Priority #3)
   - Reduce from 1.2x ATR to 1.0x ATR
   - Limit max stop loss to $0.50/share for high-volatility stocks

### Phase 2: Entry Improvements (Implement After Phase 1)
4. **Tighten Entry Filters**
   - Increase VWAP extension: 0.5x → 0.7x ATR
   - Increase RSI threshold: 55 → 60
   - Require death candle pattern (don't allow entries without pattern)
   - Increase volume requirement: 0.5x → 1.0x average

5. **Improve Entry Timing**
   - Move entry cutoff from 2:00 PM to 1:00 PM
   - Avoid entries in first 30 min (9:30-10:00 AM)

### Phase 3: Optimization (After Phase 1 & 2 Show Improvement)
6. **Reduce Commission Impact**
   - Increase minimum position size
   - Filter out stocks < $5

7. **Increase Trade Frequency** (if win rate improves)
   - Increase max concurrent positions: 3 → 4-5
   - Slightly relax entry criteria if needed

---

## Expected Impact of Fixes

**If we eliminate EOD losses:**
- Current: $1,287.88 P&L
- Eliminate EOD: +$373.69 (EOD losses)
- **New P&L: $1,661.57** ✅ **EXCEEDS $1,500 TARGET!**

**If we improve win rate from 35.7% to 45%:**
- More trades hitting Target 1 (current: 13 trades, 31% of total)
- At 45% win rate, expect ~19 Target 1 hits vs 13
- **Additional ~6 Target 1 hits × $176 avg = +$1,056**
- **Combined P&L: $2,343.88** ✅✅ **Well above target!**

**Combined impact (fix EOD + improve win rate):**
- **Projected P&L: ~$2,000-$2,500** ✅✅
- **Well above $1,500 profit target!**

---

## Conclusion

The strategy has **strong fundamentals**:
- Target exits work extremely well (92.3% win rate)
- Win/Loss ratio is excellent (3.70)
- Entry logic captures good opportunities

**Main issues are operational:**
1. EOD exit logic allowing too many losing positions to hold until close
2. Entry criteria too relaxed, leading to low win rate
3. Trailing stops hurting performance

**With these fixes, the bot should easily exceed the $1,500 profit target.**

---

## Files to Modify

1. **pkg/strategy/exit.go** - Fix EOD exit logic, disable/adjust trailing stops
2. **pkg/strategy/entry.go** - Tighten entry filters, improve timing
3. **pkg/strategy/position.go** - Adjust stop loss calculation if needed

---

Generated: 2025-11-21
Based on analysis of: backtest_20251121_122054_run*.csv (10 runs, 23 days each)

