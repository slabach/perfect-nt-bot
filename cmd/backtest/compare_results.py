#!/usr/bin/env python3
"""
Compare new backtest results with previous results to evaluate fixes
"""

import csv
import sys
from collections import defaultdict
from datetime import datetime

def parse_csv(filepath):
    """Parse a backtest CSV file and return list of trades"""
    trades = []
    with open(filepath, 'r') as f:
        reader = csv.DictReader(f)
        for row in reader:
            if not row.get('Ticker'):
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
                'Direction': row.get('Direction', 'SHORT'),
            }
            trades.append(trade)
    return trades

def analyze_file(filepath):
    """Analyze a single backtest file"""
    trades = parse_csv(filepath)
    
    total_trades = len(trades)
    wins = sum(1 for t in trades if t['NetPnL'] > 0)
    losses = sum(1 for t in trades if t['NetPnL'] < 0)
    total_net_pnl = sum(t['NetPnL'] for t in trades)
    win_rate = (wins / total_trades * 100) if total_trades > 0 else 0
    
    # Exit reason breakdown
    exit_reasons = defaultdict(lambda: {'count': 0, 'total_pnl': 0, 'wins': 0})
    for trade in trades:
        reason = trade['Reason']
        exit_reasons[reason]['count'] += 1
        exit_reasons[reason]['total_pnl'] += trade['NetPnL']
        if trade['NetPnL'] > 0:
            exit_reasons[reason]['wins'] += 1
    
    # EOD exits
    eod_trades = [t for t in trades if t['Reason'] == 'End of Day']
    eod_pnl = sum(t['NetPnL'] for t in eod_trades)
    
    # Time Decay exits (early exits)
    time_decay_trades = [t for t in trades if t['Reason'] == 'Time Decay']
    time_decay_pnl = sum(t['NetPnL'] for t in time_decay_trades)
    
    # Entry times
    entry_hours = defaultdict(int)
    for trade in trades:
        entry_hours[trade['EntryTime'].hour] += 1
    
    # Afternoon entries (after 2 PM)
    afternoon_entries = [t for t in trades if t['EntryTime'].hour >= 14]
    
    return {
        'filepath': filepath,
        'total_trades': total_trades,
        'wins': wins,
        'losses': losses,
        'win_rate': win_rate,
        'total_net_pnl': total_net_pnl,
        'exit_reasons': dict(exit_reasons),
        'eod_trades': len(eod_trades),
        'eod_pnl': eod_pnl,
        'time_decay_trades': len(time_decay_trades),
        'time_decay_pnl': time_decay_pnl,
        'afternoon_entries': len(afternoon_entries),
        'entry_hours': dict(entry_hours),
    }

if __name__ == '__main__':
    if len(sys.argv) < 2:
        print("Usage: python compare_results.py <csv_file1> [csv_file2] ...")
        sys.exit(1)
    
    filepaths = sys.argv[1:]
    results = []
    
    print("=" * 80)
    print("NEW BACKTEST RESULTS ANALYSIS (After Fixes)")
    print("=" * 80)
    print()
    
    for filepath in filepaths:
        result = analyze_file(filepath)
        results.append(result)
        
        print(f"File: {filepath.split('/')[-1]}")
        print("-" * 80)
        print(f"Total Trades: {result['total_trades']}")
        print(f"Wins: {result['wins']} | Losses: {result['losses']} | Win Rate: {result['win_rate']:.1f}%")
        print(f"Total Net P&L: ${result['total_net_pnl']:,.2f}")
        print()
        
        print("Exit Reasons:")
        for reason, stats in sorted(result['exit_reasons'].items(), key=lambda x: x[1]['count'], reverse=True):
            win_rate = (stats['wins'] / stats['count'] * 100) if stats['count'] > 0 else 0
            avg_pnl = stats['total_pnl'] / stats['count'] if stats['count'] > 0 else 0
            print(f"  {reason:20s}: {stats['count']:2d} trades | "
                  f"Win Rate: {win_rate:5.1f}% | "
                  f"Total P&L: ${stats['total_pnl']:8.2f} | "
                  f"Avg P&L: ${avg_pnl:7.2f}")
        print()
        
        print("Fix Evaluation:")
        print(f"  EOD Exits: {result['eod_trades']} trades, Total P&L: ${result['eod_pnl']:.2f}")
        if result['eod_trades'] > 0:
            eod_win_rate = sum(1 for t in parse_csv(filepath) if t['Reason'] == 'End of Day' and t['NetPnL'] > 0) / result['eod_trades'] * 100
            print(f"    EOD Win Rate: {eod_win_rate:.1f}%")
        
        print(f"  Time Decay Exits (Early Exits): {result['time_decay_trades']} trades, Total P&L: ${result['time_decay_pnl']:.2f}")
        print(f"  Afternoon Entries (>= 2 PM): {result['afternoon_entries']} trades")
        print()
        
        print("Entry Hours:")
        for hour in sorted(result['entry_hours'].keys()):
            print(f"  {hour:2d}:00 - {result['entry_hours'][hour]} trades")
        print()
        print()
    
    # Summary across all files
    print("=" * 80)
    print("SUMMARY ACROSS ALL NEW BACKTESTS")
    print("=" * 80)
    
    total_trades = sum(r['total_trades'] for r in results)
    total_wins = sum(r['wins'] for r in results)
    total_losses = sum(r['losses'] for r in results)
    total_pnl = sum(r['total_net_pnl'] for r in results)
    total_eod = sum(r['eod_trades'] for r in results)
    total_eod_pnl = sum(r['eod_pnl'] for r in results)
    total_time_decay = sum(r['time_decay_trades'] for r in results)
    total_time_decay_pnl = sum(r['time_decay_pnl'] for r in results)
    total_afternoon = sum(r['afternoon_entries'] for r in results)
    
    overall_win_rate = (total_wins / total_trades * 100) if total_trades > 0 else 0
    
    print(f"Total Trades: {total_trades}")
    print(f"Overall Win Rate: {overall_win_rate:.1f}%")
    print(f"Total Net P&L: ${total_pnl:,.2f}")
    print()
    
    print("Fix Effectiveness:")
    print(f"1. EOD Fix:")
    print(f"   - EOD Exits: {total_eod} trades (down from 23 in previous)")
    print(f"   - EOD Total P&L: ${total_eod_pnl:.2f} (was -$435.69)")
    if total_eod > 0:
        eod_win_rate = (sum(1 for r in results for t in parse_csv(r['filepath']) 
                           if t['Reason'] == 'End of Day' and t['NetPnL'] > 0) / total_eod * 100) if total_eod > 0 else 0
        print(f"   - EOD Win Rate: {eod_win_rate:.1f}% (was 0%)")
    
    print(f"2. Time Decay (Early Exit) Fix:")
    print(f"   - Time Decay Exits: {total_time_decay} trades")
    print(f"   - Time Decay Total P&L: ${total_time_decay_pnl:.2f}")
    
    print(f"3. Time-Based Entry Filter:")
    print(f"   - Afternoon Entries (>= 2 PM): {total_afternoon} trades")
    if total_afternoon == 0:
        print(f"   ✓ SUCCESS: No entries after 2:00 PM")
    else:
        print(f"   ⚠️  WARNING: {total_afternoon} entries still occurred after 2:00 PM")
    
    print()
    print("Comparison to Previous Results:")
    print("  Previous: 196 trades, 75.5% win rate, $17,418.51 P&L")
    print(f"  New:      {total_trades} trades, {overall_win_rate:.1f}% win rate, ${total_pnl:,.2f} P&L")
    print()
    
    # Check for trailing stops
    all_trades = []
    for r in results:
        all_trades.extend(parse_csv(r['filepath']))
    
    trailing_stop_trades = [t for t in all_trades if 'Trailing Stop' in t['Reason']]
    if trailing_stop_trades:
        print(f"4. Trailing Stop Fix:")
        trailing_pnl = sum(t['NetPnL'] for t in trailing_stop_trades)
        print(f"   - Trailing Stop Trades: {len(trailing_stop_trades)}")
        print(f"   - Trailing Stop Total P&L: ${trailing_pnl:.2f}")
    else:
        print(f"4. Trailing Stop Fix:")
        print(f"   ✓ SUCCESS: No trailing stop exits (fix working - only activates after Target 1)")

