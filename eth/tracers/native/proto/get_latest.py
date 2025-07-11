#!/usr/bin/env python3
"""
Script to get the latest transaction hash from protobuf files in the data directory structure.

This script finds the most recent .pb file and extracts the latest transaction hash from it,
assuming the files are organized in a way that allows finding the latest without parsing all files.

Usage:
    python get_latest.py [data_directory_path]
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


def get_latest_transaction_hash(data_dir):
    """
    Get the latest transaction hash from the most recent .pb file.
    
    Args:
        data_dir (str): Path to the data directory
        
    Returns:
        str: The latest transaction hash, or None if no transactions found
    """
    if not os.path.exists(data_dir):
        print(f"Error: Data directory '{data_dir}' does not exist.")
        return None
    
    # Find all .pb files in the data directory structure
    pb_files = []
    for root, dirs, files in os.walk(data_dir):
        # Skip blocks/ and errors/ directories
        dirs[:] = [d for d in dirs if d not in ['blocks', 'errors']]
        
        # Find all .pb files in current directory
        for file in files:
            if file.endswith('.pb'):
                file_path = os.path.join(root, file)
                pb_files.append(file_path)
    
    if not pb_files:
        print("No .pb files found in the data directory.")
        return None
    
    # Sort files by modification time to get the most recent
    pb_files.sort(key=lambda x: os.path.getmtime(x), reverse=True)
    latest_file = pb_files[0]
    
    print(f"Reading latest file: {latest_file}")
    
    try:
        with open(latest_file, 'rb') as f:
            batch = TxGasDimensionResultBatch()
            batch.ParseFromString(f.read())
            
            if not batch.results:
                print("No transactions found in the latest file.")
                return None
            
            # Get the last transaction from the batch
            latest_result = batch.results[-1]
            
            if hasattr(latest_result, 'tx_hash') and latest_result.tx_hash:
                return latest_result.tx_hash
            else:
                print("No transaction hash found in the latest transaction.")
                return None
                
    except Exception as e:
        print(f"Error reading file {latest_file}: {e}")
        return None


def main():
    """Main function to get the latest transaction hash."""
    # Get data directory from command line argument or use default
    if len(sys.argv) > 1:
        data_dir = sys.argv[1]
    else:
        data_dir = "data"
    
    print(f"Getting latest transaction from directory: {data_dir}")
    
    latest_tx_hash = get_latest_transaction_hash(data_dir)
    
    if latest_tx_hash:
        print(f"Latest transaction hash: {latest_tx_hash}")
    else:
        print("No transaction hash found.")
        sys.exit(1)


if __name__ == "__main__":
    main() 