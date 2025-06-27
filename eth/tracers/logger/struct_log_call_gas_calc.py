#!/usr/bin/env python3
# Copyright 2025, Offchain Labs, Inc.
# For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
"""
Struct Logger Call Gas Calculator

This script analyzes EVM struct logs to track gas usage of a particular call, 
at a particular program counter and depth.
"""

import argparse
import json
from typing import Dict, List, Tuple, Any

def get_struct_logs_from_file(file_path: str) -> List[Dict[str, Any]]:
    with open(file_path, "r") as f:
        data = json.load(f)
        
    # Check if structLogs exists in data['result']['structLogs']
    if 'result' in data and 'structLogs' in data['result']:
        return data['result']['structLogs']
    
    # Check if structLogs exists directly in data['structLogs']
    if 'structLogs' in data:
        return data['structLogs']
    
    # If neither exists, raise an error
    raise KeyError("structLogs not found in either data['result']['structLogs'] or data['structLogs']")

def parse_struct_logs(file_path: str, start_pc: int, depth: int) -> Dict[str, Any]:
    """
    Parse struct logs and analyze gas usage for a specific PC and depth.
    
    Args:
        file_path: Path to the struct logs JSON file
        start_pc: Starting program counter to monitor
        depth: Depth level to monitor
        
    Returns:
        Dictionary containing analysis results
    """
    stack_increasers = ["CALL", "CALLCODE", "DELEGATECALL", "STATICCALL", "CREATE", "CREATE2"]
    
    call_stack: Dict[Tuple[int, int], Dict[str, Any]] = {}
    active_calls: List[Tuple[int, int]] = []
    gas_sum = 0
    gas_start = 0
    gas_end = 0
    
    struct_logs = get_struct_logs_from_file(file_path)
    monitor = False
    
    for log in struct_logs:
        if log['pc'] == start_pc and log['depth'] == depth:
            monitor = True
            print(f"+ Observed start {log['op']} at {log['pc']}, depth {log['depth']}")
            gas_start = log['gas']
            continue
            
        if log['pc'] == start_pc + 1 and log['depth'] == depth:
            monitor = False
            print(f"+ Observed end {log['op']} at {log['pc']}, depth {log['depth']}")
            gas_end = log['gas']
            break
            
        if monitor:
            current_depth = log['depth']
            current_pc = log['pc']

            if current_depth != depth + 1:
                continue
                
            if log['op'] in stack_increasers:
                call_info = {
                    'pc': current_pc,
                    'depth': current_depth,
                    'gas_at_call': log['gas'],
                    'op': log['op']
                }
                call_stack[(current_pc, current_depth)] = call_info
                active_calls.append((current_pc, current_depth))
                print(f"Call made at PC {current_pc}, depth {current_depth}, gas {log['gas']}")
                continue
            else:
                gas_sum += log['gasCost']

                for call_key in active_calls:
                    call_pc, call_depth = call_key
                    if current_pc == call_pc + 1:
                        call_info = call_stack.pop(call_key)
                        gas_used = call_info['gas_at_call'] - log['gas']
                        call_info['gas_used'] = gas_used
                        call_info['return_pc'] = current_pc
                        call_info['return_depth'] = current_depth
                        call_stack[call_key] = call_info
                        active_calls.remove(call_key)
                        print(f"Returned from call at PC {call_pc}, depth {call_depth}, gas used: {gas_used}, gas left: {log['gas']}")

    # Add gas from completed calls
    for _, call_info in call_stack.items():
        if 'gas_used' in call_info:
            gas_sum += call_info['gas_used']

    return {
        'gas_sum': gas_sum,
        'gas_start': gas_start,
        'gas_end': gas_end,
        'call_stack': call_stack
    }


def print_results(results: Dict[str, Any]) -> None:
    """
    Print the analysis results in a formatted way.
    
    Args:
        results: Dictionary containing analysis results
    """
    gas_sum = results['gas_sum']
    gas_start = results['gas_start']
    gas_end = results['gas_end']
    call_stack = results['call_stack']
    
    print(f"> Execution gas used: {gas_sum}")
    gas_total = gas_start - gas_end
    print(f"> Total gas used: {gas_total} ({gas_start} - {gas_end})")
    print(f"> Non Execution gas used: {gas_total - gas_sum}")
    print(f"Call stack details:")
    
    for call_key, call_info in call_stack.items():
        print(f"  Call at PC {call_info['pc']}, depth {call_info['depth']}: {call_info['op']}")
        if 'gas_used' in call_info:
            print(f"    Gas used: {call_info['gas_used']}")
            print(f"    Returned at PC {call_info['return_pc']}, depth {call_info['return_depth']}")
        else:
            print(f"    Still active")


def main() -> None:
    """Main function to parse command line arguments and run the analysis."""
    parser = argparse.ArgumentParser(
        description="Parse EVM struct logs to analyze gas usage and call stack",
        formatter_class=argparse.ArgumentDefaultsHelpFormatter
    )
    parser.add_argument(
        "file",
        help="Path to the struct logs JSON file"
    )
    parser.add_argument(
        "--start-pc",
        type=int,
        default=15413,
        help="Starting program counter to monitor"
    )
    parser.add_argument(
        "--depth",
        type=int,
        default=3,
        help="Depth level to monitor"
    )
    args = parser.parse_args()
    
    try:
        results = parse_struct_logs(args.file, args.start_pc, args.depth)
        print_results(results)
    except FileNotFoundError:
        print(f"Error: File '{args.file}' not found.")
        exit(1)
    except json.JSONDecodeError:
        print(f"Error: Invalid JSON in file '{args.file}'.")
        exit(1)
    except KeyError as e:
        print(f"Error: Missing required key '{e}' in JSON structure.")
        exit(1)
    except Exception as e:
        print(f"Error: {e}")
        exit(1)


if __name__ == "__main__":
    main()