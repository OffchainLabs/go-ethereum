#!/usr/bin/env python3
"""
Script to count transactions from protobuf files in the data directory structure.

This script iterates over the data folder, ignoring the blocks/ and errors/ folders,
and reads all .pb files to count the total number of transactions saved to disk.

Usage:
    python count_transactions.py [data_directory_path]
"""

import os
import sys
import glob
from pathlib import Path

# Import the generated protobuf classes
try:
    from gas_dimension_pb2 import TxGasDimensionResultBatch
except ImportError:
    print("Error: Could not import gas_dimension_pb2 module.")
    print("Make sure the protobuf file is in the same directory as this script.")
    print("You may need to run: pip install protobuf")
    sys.exit(1)


def count_transactions_in_file(file_path):
    """
    Count transactions in a single protobuf file and return transaction details.
    
    Args:
        file_path (str): Path to the protobuf file
        
    Returns:
        tuple: (transaction_count, list of (block_number, tx_hash) tuples)
    """
    try:
        with open(file_path, 'rb') as f:
            batch = TxGasDimensionResultBatch()
            batch.ParseFromString(f.read())
            
            transaction_count = len(batch.results)
            transaction_details = []
            
            for result in batch.results:
                if hasattr(result, 'tx_hash') and result.tx_hash:
                    transaction_details.append((result.block_number, result.tx_hash))
            
            return transaction_count, transaction_details
    except Exception as e:
        print(f"Error reading file {file_path}: {e}")
        return 0, []


def count_all_transactions(data_dir):
    """
    Count all transactions in the data directory structure.
    
    Args:
        data_dir (str): Path to the data directory
        
    Returns:
        tuple: (total_transactions, total_files, file_details, all_transaction_details)
    """
    if not os.path.exists(data_dir):
        print(f"Error: Data directory '{data_dir}' does not exist.")
        return 0, 0, [], []
    
    total_transactions = 0
    total_files = 0
    file_details = []
    all_transaction_details = []
    
    # Walk through the data directory
    for root, dirs, files in os.walk(data_dir):
        # Skip blocks/ and errors/ directories
        dirs[:] = [d for d in dirs if d not in ['blocks', 'errors']]
        
        # Find all .pb files in current directory
        pb_files = [f for f in files if f.endswith('.pb')]
        
        for pb_file in pb_files:
            file_path = os.path.join(root, pb_file)
            transaction_count, transaction_details = count_transactions_in_file(file_path)
            
            if transaction_count > 0:
                total_transactions += transaction_count
                total_files += 1
                file_details.append({
                    'file': file_path,
                    'transactions': transaction_count
                })
                all_transaction_details.extend(transaction_details)
                print(f"  {file_path}: {transaction_count} transactions")
    
    return total_transactions, total_files, file_details, all_transaction_details


def get_last_transaction_hash(transaction_details):
    """
    Get the last transaction hash in chronological order.
    
    Args:
        transaction_details (list): List of (block_number, tx_hash) tuples
        
    Returns:
        str: The last transaction hash in chronological order, or None if no transactions
    """
    if not transaction_details:
        return None
    
    # Sort by block number first, then by transaction hash for consistency
    sorted_transactions = sorted(transaction_details, key=lambda x: (x[0], x[1]))
    
    # Return the hash of the last transaction
    return sorted_transactions[-1][1]


def main():
    """Main function to run the transaction counting script."""
    # Get data directory from command line argument or use default
    if len(sys.argv) > 1:
        data_dir = sys.argv[1]
    else:
        data_dir = "data"
    
    print(f"Counting transactions in directory: {data_dir}")
    print("=" * 50)
    
    total_transactions, total_files, file_details, transaction_details = count_all_transactions(data_dir)
    
    print("=" * 50)
    print(f"Summary:")
    print(f"  Total files processed: {total_files}")
    print(f"  Total transactions: {total_transactions:,}")
    
    if total_files > 0:
        avg_transactions_per_file = total_transactions / total_files
        print(f"  Average transactions per file: {avg_transactions_per_file:.1f}")
    
    # Get and print the last transaction hash in chronological order
    last_tx_hash = get_last_transaction_hash(transaction_details)
    if last_tx_hash:
        print(f"  Last transaction hash (chronological): {last_tx_hash}")
    else:
        print("  No transaction hashes found")
    
    # Show breakdown by block group if available
    if file_details:
        print("\nBreakdown by block group:")
        block_groups = {}
        for detail in file_details:
            # Extract block group from path (e.g., data/1000/batch_123.pb -> 1000)
            path_parts = detail['file'].split(os.sep)
            if len(path_parts) >= 2:
                block_group = path_parts[-2]  # Second to last part
                if block_group not in block_groups:
                    block_groups[block_group] = 0
                block_groups[block_group] += detail['transactions']
        
        for block_group in sorted(block_groups.keys(), key=int):
            print(f"  Block group {block_group}: {block_groups[block_group]:,} transactions")


if __name__ == "__main__":
    main() 