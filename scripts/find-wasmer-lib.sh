#!/bin/sh
set -e

case "$1" in
    "save")
        # First, find all potential libwasmer.so files
        WASMER_PATHS=$(find /go/pkg/mod -name "libwasmer.so" -type f | grep iden3/wasmer-go)
        
        if [ -z "$WASMER_PATHS" ]; then
            echo "Error: No libwasmer.so found in go mod cache" >&2
            exit 1
        fi
        
        echo "Found libwasmer.so files:"
        echo "$WASMER_PATHS"
        
        # Copy the first one to system location for the build
        FIRST_WASMER=$(echo "$WASMER_PATHS" | head -1)
        cp "$FIRST_WASMER" /usr/lib/libwasmer.so
        echo "Copied $FIRST_WASMER to /usr/lib/libwasmer.so for build"
        ;;
    "save-after-build")
        # After building, use ldd to find which libwasmer.so the binary actually uses
        if [ ! -f "/src/davinci-sequencer" ]; then
            echo "Error: davinci-sequencer binary not found" >&2
            exit 1
        fi
        
        # Use ldd to find the actual libwasmer.so path the binary expects
        LDD_OUTPUT=$(ldd /src/davinci-sequencer | grep libwasmer.so || true)
        
        if [ -z "$LDD_OUTPUT" ]; then
            echo "Error: libwasmer.so not found in binary dependencies" >&2
            exit 1
        fi
        
        echo "Binary dependency: $LDD_OUTPUT"
        
        # Extract the expected path from ldd output
        EXPECTED_PATH=$(echo "$LDD_OUTPUT" | awk '{print $3}' | head -1)
        
        if [ -z "$EXPECTED_PATH" ] || [ "$EXPECTED_PATH" = "not" ]; then
            # If ldd shows "not found", extract the library name and find it
            LIB_NAME=$(echo "$LDD_OUTPUT" | awk '{print $1}')
            echo "Library $LIB_NAME not found, searching in go mod cache..."
            
            # Find the correct libwasmer.so that matches the architecture
            WASMER_PATH=$(find /go/pkg/mod -name "libwasmer.so" -type f | grep iden3/wasmer-go | head -1)
            
            if [ -z "$WASMER_PATH" ]; then
                echo "Error: Could not find libwasmer.so in go mod cache" >&2
                exit 1
            fi
            
            # Extract the relative path from /go/pkg/mod
            WASMER_REL_PATH=${WASMER_PATH#/go/pkg/mod/}
        else
            # Extract the relative path from the expected path
            WASMER_REL_PATH=${EXPECTED_PATH#/go/pkg/mod/}
            WASMER_PATH="/go/pkg/mod/$WASMER_REL_PATH"
        fi
        
        echo "Using libwasmer.so from: $WASMER_PATH"
        echo "Relative path: $WASMER_REL_PATH"
        
        # Save the library and its path for the final stage
        cp "$WASMER_PATH" /src/libwasmer.so
        echo "$WASMER_REL_PATH" > /src/wasmer_path.txt
        echo "Saved libwasmer.so and path info to /src/"
        ;;
    "restore")
        # Restore the library in the final image
        if [ -f /app/wasmer_path.txt ] && [ -f /app/libwasmer.so ]; then
            DEST_PATH="/go/pkg/mod/$(cat /app/wasmer_path.txt)"
            mkdir -p "$(dirname "$DEST_PATH")"
            cp /app/libwasmer.so "$DEST_PATH"
            echo "Restored libwasmer.so to: $DEST_PATH"
        else
            echo "Error: Required files not found in /app" >&2
            exit 1
        fi
        ;;
    *)
        echo "Usage: $0 {save|save-after-build|restore}" >&2
        exit 1
        ;;
esac
