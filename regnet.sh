#!/bin/bash

# Function to print usage message
print_usage() {
    echo "Usage:"
    echo "$0 start nodex"
    echo "OR"
    echo "$0 cli nodex <command> [command_flags]"
    echo ""
    echo "For nodex, x is a number like node1, node2, node3 etc."
    exit 1
}

# Check for -h or --help
if [[ "$1" == "-h" || "$1" == "--help" ]]; then
    print_usage
fi

# Check if at least two parameters are provided
if [[ $# -lt 2 ]]; then
    print_usage
fi

NODE_PREFIX="node"
if [[ "$2" =~ ^${NODE_PREFIX}([0-9]+)$ ]]; then
    NODE_NUMBER=${BASH_REMATCH[1]}
else
    echo "Invalid node name. Expected format is nodex where x is a number."
    exit 1
fi

# Based on the first parameter
if [[ "$1" == "start" ]]; then
    # Remove the first two arguments (script name and "start")
    shift 2
    # Check if it is node1 to apply specific flags
    if [[ "$NODE_NUMBER" -eq 1 ]]; then
       # Pass all additional arguments to ilxd
       ilxd --regtest --regtestval --loglevel=debug --datadir="$HOME/.regnet/${NODE_PREFIX}1" "$@"
    else
       LISTEN_PORT=$((9002 + NODE_NUMBER))
       GRPC_PORT=$((5000 + NODE_NUMBER))
       # Pass all additional arguments to ilxd
       ilxd --regtest --loglevel=debug --listenaddr=/ip4/127.0.0.1/tcp/${LISTEN_PORT} --grpclisten=/ip4/127.0.0.1/tcp/${GRPC_PORT} --datadir="$HOME/.regnet/${NODE_PREFIX}${NODE_NUMBER}" "$@"
    fi

elif [[ "$1" == "cli" ]]; then
    if [[ $# -lt 3 ]]; then
        echo "Usage for cli: $0 cli nodex <command> [command_flags]"
        exit 1
    fi

    GRPC_PORT=$((5000 + NODE_NUMBER))
    shift 2
    ilxcli --serveraddr=/ip4/127.0.0.1/tcp/${GRPC_PORT} --rpccert="$HOME/.regnet/${NODE_PREFIX}${NODE_NUMBER}/rpc.cert" "$@"

else
    echo "Invalid command. Use 'start' or 'cli'."
    exit 1
fi
