#!/usr/bin/env python3
"""
Analyze backtest CSV files to identify improvement opportunities
"""

import csv
import sys
from collections import defaultdict
from datetime import datetime
from pathlib import Path

def parse_csv(filepath):
    """Parse a backtest CSV file and return list of trades"""
    trades = []
    with open(filepath, 'r') as f:
        reader = csv.DictReader(f)
        for row in reader:
            if not row.get('Ticker'):  # Skip empty rows
                continue
            trade = {
                'Ticker': row['Ticker'],
                'EntryTime': datetime.fromisoformat(row['EntryTime'].replace('Z', '+00:00')),
                'ExitTime': datetime.fromisoformat(row['ExitTime'].replace('Z', '+00:00')),
                'EntryPrice': float(row['EntryPrice']),
                'ExitPrice': float(row['ExitPrice']),
                'Shares': int(row['Shares']),
                'Reason': row['Reason'],
                'GrossPnL': float(row['GrossPnL']),
                'Commission': float(row['Commission']),
                'NetPnL': float(row['NetPnL']),
                'Direction': row.get('Direction', 'SHORT'),  # Default to SHORT if missing
            }
            trades.append(trade)
    return trades

def analyze_backtests(filepaths):
    """Analyze multiple backtest files"""
    all_trades = []
    file_stats = {}
    
    for filepath in filepaths:
        trades = parse_csv(filepath)
        all_trades.extend(trades)
        
        # Calculate stats for this file
        total_trades = len(trades)
        wins = sum(1 for t in trades if t['NetPnL'] > 0)
        losses = sum(1 for t in trades if t['NetPnL'] < 0)
        total_net_pnl = sum(t['NetPnL'] for t in trades)
        total_gross_pnl = sum(t['GrossPnL'] for t in trades)
        total_commission = sum(t['Commission'] for t in trades)
        win_rate = (wins / total_trades * 100) if total_trades > 0 else 0
        
        # Calculate average win/loss
        winning_trades = [t for t in trades if t['NetPnL'] > 0]
        losing_trades = [t for t in trades if t['NetPnL'] < 0]
        avg_win = sum(t['NetPnL'] for t in winning_trades) / len(winning_trades) if winning_trades else 0
        avg_loss = sum(t['NetPnL'] for t in losing_trades) / len(losing_trades) if losing_trades else 0
        
        # Largest win/loss
        largest_win = max((t['NetPnL'] for t in trades), default=0)
        largest_loss = min((t['NetPnL'] for t in trades), default=0)
        
        file_stats[filepath] = {
            'total_trades': total_trades,
            'wins': wins,
            'losses': losses,
            'win_rate': win_rate,
            'total_net_pnl': total_net_pnl,
            'total_gross_pnl': total_gross_pnl,
            'total_commission': total_commission,
            'avg_win': avg_win,
            'avg_loss': avg_loss,
            'largest_win': largest_win,
            'largest_loss': largest_loss,
            'trades': trades,
        }
    
    return all_trades, file_stats

def print_analysis(file_stats):
    """Print comprehensive analysis"""
    print("=" * 80)
    print("BACKTEST ANALYSIS - IMPROVEMENT OPPORTUNITIES")
    print("=" * 80)
    print()
    
    # Overall statistics
    print("OVERALL STATISTICS")
    print("-" * 80)
    all_trades = []
    for stats in file_stats.values():
        all_trades.extend(stats['trades'])
    
    total_trades = len(all_trades)
    wins = sum(1 for t in all_trades if t['NetPnL'] > 0)
    losses = sum(1 for t in all_trades if t['NetPnL'] < 0)
    total_net_pnl = sum(t['NetPnL'] for t in all_trades)
    total_commission = sum(t['Commission'] for t in all_trades)
    win_rate = (wins / total_trades * 100) if total_trades > 0 else 0
    
    winning_trades = [t for t in all_trades if t['NetPnL'] > 0]
    losing_trades = [t for t in all_trades if t['NetPnL'] < 0]
    avg_win = sum(t['NetPnL'] for t in winning_trades) / len(winning_trades) if winning_trades else 0
    avg_loss = sum(t['NetPnL'] for t in losing_trades) / len(losing_trades) if losing_trades else 0
    
    print(f"Total Trades: {total_trades}")
    print(f"Wins: {wins} ({win_rate:.1f}%)")
    print(f"Losses: {losses} ({100-win_rate:.1f}%)")
    print(f"Total Net P&L: ${total_net_pnl:,.2f}")
    print(f"Total Commission: ${total_commission:,.2f}")
    print(f"Average Win: ${avg_win:.2f}")
    print(f"Average Loss: ${avg_loss:.2f}")
    print(f"Win/Loss Ratio: {abs(avg_win/avg_loss):.2f}" if avg_loss != 0 else "N/A")
    print()
    
    # Exit reason analysis
    print("EXIT REASON ANALYSIS")
    print("-" * 80)
    exit_reasons = defaultdict(lambda: {'count': 0, 'total_pnl': 0, 'wins': 0, 'losses': 0})
    for trade in all_trades:
        reason = trade['Reason']
        exit_reasons[reason]['count'] += 1
        exit_reasons[reason]['total_pnl'] += trade['NetPnL']
        if trade['NetPnL'] > 0:
            exit_reasons[reason]['wins'] += 1
        else:
            exit_reasons[reason]['losses'] += 1
    
    for reason, stats in sorted(exit_reasons.items(), key=lambda x: x[1]['count'], reverse=True):
        win_rate = (stats['wins'] / stats['count'] * 100) if stats['count'] > 0 else 0
        avg_pnl = stats['total_pnl'] / stats['count'] if stats['count'] > 0 else 0
        print(f"{reason:20s}: {stats['count']:3d} trades | "
              f"Win Rate: {win_rate:5.1f}% | "
              f"Total P&L: ${stats['total_pnl']:8.2f} | "
              f"Avg P&L: ${avg_pnl:7.2f}")
    print()
    
    # End of Day analysis
    eod_trades = [t for t in all_trades if t['Reason'] == 'End of Day']
    if eod_trades:
        print("END OF DAY EXITS - IMPROVEMENT OPPORTUNITY")
        print("-" * 80)
        eod_pnl = sum(t['NetPnL'] for t in eod_trades)
        eod_wins = sum(1 for t in eod_trades if t['NetPnL'] > 0)
        eod_win_rate = (eod_wins / len(eod_trades) * 100) if eod_trades else 0
        print(f"EOD Trades: {len(eod_trades)} ({len(eod_trades)/total_trades*100:.1f}% of all trades)")
        print(f"EOD Total P&L: ${eod_pnl:,.2f}")
        print(f"EOD Win Rate: {eod_win_rate:.1f}%")
        print(f"⚠️  ISSUE: {len(eod_trades)} trades closed at EOD - consider earlier exits or holding overnight")
        print()
    
    # Stop Loss analysis
    stop_loss_trades = [t for t in all_trades if 'Stop Loss' in t['Reason']]
    if stop_loss_trades:
        print("STOP LOSS ANALYSIS")
        print("-" * 80)
        sl_pnl = sum(t['NetPnL'] for t in stop_loss_trades)
        sl_avg_loss = sum(t['NetPnL'] for t in stop_loss_trades) / len(stop_loss_trades) if stop_loss_trades else 0
        largest_sl = min((t['NetPnL'] for t in stop_loss_trades), default=0)
        print(f"Stop Loss Trades: {len(stop_loss_trades)}")
        print(f"Total P&L from Stops: ${sl_pnl:,.2f}")
        print(f"Average Loss: ${sl_avg_loss:.2f}")
        print(f"Largest Stop Loss: ${largest_sl:.2f}")
        
        # Find problematic stop losses
        large_stops = [t for t in stop_loss_trades if t['NetPnL'] < -100]
        if large_stops:
            print(f"⚠️  ISSUE: {len(large_stops)} stop losses > $100")
            print("   Consider: Tighter stops, better entry timing, or position sizing")
        print()
    
    # Trailing Stop analysis
    trailing_stop_trades = [t for t in all_trades if 'Trailing Stop' in t['Reason']]
    if trailing_stop_trades:
        print("TRAILING STOP ANALYSIS")
        print("-" * 80)
        ts_pnl = sum(t['NetPnL'] for t in trailing_stop_trades)
        ts_win_rate = (sum(1 for t in trailing_stop_trades if t['NetPnL'] > 0) / len(trailing_stop_trades) * 100) if trailing_stop_trades else 0
        print(f"Trailing Stop Trades: {len(trailing_stop_trades)}")
        print(f"Total P&L: ${ts_pnl:,.2f}")
        print(f"Win Rate: {ts_win_rate:.1f}%")
        if ts_pnl < 0:
            print("⚠️  ISSUE: Trailing stops are losing money - consider adjusting trailing stop logic")
        print()
    
    # Target analysis
    target_trades = [t for t in all_trades if 'Target' in t['Reason']]
    if target_trades:
        print("TARGET EXITS ANALYSIS")
        print("-" * 80)
        target_pnl = sum(t['NetPnL'] for t in target_trades)
        target_win_rate = (sum(1 for t in target_trades if t['NetPnL'] > 0) / len(target_trades) * 100) if target_trades else 0
        print(f"Target Exits: {len(target_trades)} ({len(target_trades)/total_trades*100:.1f}% of all trades)")
        print(f"Total P&L: ${target_pnl:,.2f}")
        print(f"Win Rate: {target_win_rate:.1f}%")
        print(f"✓ Target exits are working well")
        print()
    
    # Commission impact
    print("COMMISSION IMPACT")
    print("-" * 80)
    commission_pct = (total_commission / abs(total_net_pnl) * 100) if total_net_pnl != 0 else 0
    print(f"Total Commission: ${total_commission:,.2f}")
    print(f"Commission as % of Net P&L: {commission_pct:.2f}%")
    if commission_pct > 10:
        print("⚠️  ISSUE: High commission impact - consider reducing trade frequency or increasing position size")
    print()
    
    # Time-based patterns
    print("TIME-BASED PATTERNS")
    print("-" * 80)
    entry_hours = defaultdict(lambda: {'count': 0, 'total_pnl': 0})
    exit_hours = defaultdict(lambda: {'count': 0, 'total_pnl': 0})
    
    for trade in all_trades:
        entry_hour = trade['EntryTime'].hour
        exit_hour = trade['ExitTime'].hour
        entry_hours[entry_hour]['count'] += 1
        entry_hours[entry_hour]['total_pnl'] += trade['NetPnL']
        exit_hours[exit_hour]['count'] += 1
        exit_hours[exit_hour]['total_pnl'] += trade['NetPnL']
    
    print("Entry Hour Performance:")
    for hour in sorted(entry_hours.keys()):
        stats = entry_hours[hour]
        avg_pnl = stats['total_pnl'] / stats['count'] if stats['count'] > 0 else 0
        print(f"  {hour:2d}:00 - {stats['count']:3d} trades, Avg P&L: ${avg_pnl:7.2f}, Total: ${stats['total_pnl']:8.2f}")
    print()
    
    # Ticker performance
    print("TICKER PERFORMANCE (Top 10 by trade count)")
    print("-" * 80)
    ticker_stats = defaultdict(lambda: {'count': 0, 'total_pnl': 0, 'wins': 0})
    for trade in all_trades:
        ticker = trade['Ticker']
        ticker_stats[ticker]['count'] += 1
        ticker_stats[ticker]['total_pnl'] += trade['NetPnL']
        if trade['NetPnL'] > 0:
            ticker_stats[ticker]['wins'] += 1
    
    sorted_tickers = sorted(ticker_stats.items(), key=lambda x: x[1]['count'], reverse=True)[:10]
    for ticker, stats in sorted_tickers:
        win_rate = (stats['wins'] / stats['count'] * 100) if stats['count'] > 0 else 0
        avg_pnl = stats['total_pnl'] / stats['count'] if stats['count'] > 0 else 0
        print(f"{ticker:6s}: {stats['count']:3d} trades | "
              f"Win Rate: {win_rate:5.1f}% | "
              f"Total P&L: ${stats['total_pnl']:8.2f} | "
              f"Avg P&L: ${avg_pnl:7.2f}")
    print()
    
    # Large losses
    print("LARGEST LOSSES (Top 10)")
    print("-" * 80)
    sorted_losses = sorted([t for t in all_trades if t['NetPnL'] < 0], key=lambda x: x['NetPnL'])[:10]
    for trade in sorted_losses:
        print(f"{trade['Ticker']:6s} | {trade['Reason']:20s} | "
              f"Entry: ${trade['EntryPrice']:7.2f} | Exit: ${trade['ExitPrice']:7.2f} | "
              f"P&L: ${trade['NetPnL']:8.2f} | Shares: {trade['Shares']:4d}")
    print()
    
    # Large wins
    print("LARGEST WINS (Top 10)")
    print("-" * 80)
    sorted_wins = sorted([t for t in all_trades if t['NetPnL'] > 0], key=lambda x: x['NetPnL'], reverse=True)[:10]
    for trade in sorted_wins:
        print(f"{trade['Ticker']:6s} | {trade['Reason']:20s} | "
              f"Entry: ${trade['EntryPrice']:7.2f} | Exit: ${trade['ExitPrice']:7.2f} | "
              f"P&L: ${trade['NetPnL']:8.2f} | Shares: {trade['Shares']:4d}")
    print()
    
    # Trade duration analysis
    print("TRADE DURATION ANALYSIS")
    print("-" * 80)
    durations = []
    for trade in all_trades:
        duration = (trade['ExitTime'] - trade['EntryTime']).total_seconds() / 60  # minutes
        durations.append((duration, trade))
    
    durations.sort(key=lambda x: x[0])
    avg_duration = sum(d[0] for d in durations) / len(durations) if durations else 0
    
    # Analyze by duration buckets
    short_trades = [d for d in durations if d[0] < 30]  # < 30 min
    medium_trades = [d for d in durations if 30 <= d[0] < 120]  # 30 min - 2 hours
    long_trades = [d for d in durations if d[0] >= 120]  # >= 2 hours
    
    print(f"Average Duration: {avg_duration:.1f} minutes")
    print(f"Short trades (<30min): {len(short_trades)} trades, "
          f"Avg P&L: ${sum(t[1]['NetPnL'] for t in short_trades)/len(short_trades):.2f}" if short_trades else "0 trades")
    print(f"Medium trades (30min-2hr): {len(medium_trades)} trades, "
          f"Avg P&L: ${sum(t[1]['NetPnL'] for t in medium_trades)/len(medium_trades):.2f}" if medium_trades else "0 trades")
    print(f"Long trades (>=2hr): {len(long_trades)} trades, "
          f"Avg P&L: ${sum(t[1]['NetPnL'] for t in long_trades)/len(long_trades):.2f}" if long_trades else "0 trades")
    print()
    
    # Summary of improvements
    print("=" * 80)
    print("KEY IMPROVEMENT OPPORTUNITIES")
    print("=" * 80)
    improvements = []
    
    if eod_trades and len(eod_trades) > total_trades * 0.2:
        improvements.append(f"1. EOD EXITS: {len(eod_trades)} trades ({len(eod_trades)/total_trades*100:.1f}%) closed at EOD")
        improvements.append("   → Consider: Earlier exit signals, holding overnight for winners, or tighter EOD rules")
    
    if stop_loss_trades and sl_avg_loss < -50:
        improvements.append(f"2. STOP LOSSES: Average loss ${sl_avg_loss:.2f} is large")
        improvements.append("   → Consider: Tighter stops, better entry timing, or position sizing adjustments")
    
    if trailing_stop_trades and sum(t['NetPnL'] for t in trailing_stop_trades) < 0:
        improvements.append("3. TRAILING STOPS: Currently losing money")
        improvements.append("   → Consider: Adjusting trailing stop distance or activation threshold")
    
    if commission_pct > 10:
        improvements.append(f"4. COMMISSIONS: {commission_pct:.1f}% of net P&L lost to commissions")
        improvements.append("   → Consider: Reducing trade frequency or increasing position sizes")
    
    if win_rate < 50:
        improvements.append(f"5. WIN RATE: {win_rate:.1f}% is below 50%")
        improvements.append("   → Consider: Improving entry filters, better pattern recognition, or stricter entry criteria")
    
    if abs(avg_win / avg_loss) < 1.5 if avg_loss != 0 else False:
        improvements.append(f"6. RISK/REWARD: Win/Loss ratio {abs(avg_win/avg_loss):.2f} is low")
        improvements.append("   → Consider: Letting winners run longer or cutting losses faster")
    
    if not improvements:
        improvements.append("No major issues identified. Strategy appears well-balanced.")
    
    for imp in improvements:
        print(imp)
    print()

if __name__ == '__main__':
    if len(sys.argv) < 2:
        print("Usage: python analyze_backtests.py <csv_file1> [csv_file2] ...")
        sys.exit(1)
    
    filepaths = sys.argv[1:]
    all_trades, file_stats = analyze_backtests(filepaths)
    print_analysis(file_stats)

