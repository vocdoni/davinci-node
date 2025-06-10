#!/bin/bash

echo "Testing Davinci Sequencer Dashboard setup..."
echo ""

# Check if we're in the webapp directory
if [ ! -f "package.json" ]; then
    echo "Error: Not in webapp directory. Please run this script from the webapp folder."
    exit 1
fi

# Check if pnpm is installed
if ! command -v pnpm &> /dev/null; then
    echo "Error: pnpm is not installed. Please install it with: npm install -g pnpm"
    exit 1
fi

# Check if node is installed
if ! command -v node &> /dev/null; then
    echo "Error: Node.js is not installed."
    exit 1
fi

echo "✓ Prerequisites checked"
echo ""

# Check if .env exists, if not copy from example
if [ ! -f ".env" ]; then
    echo "Creating .env file from .env.example..."
    cp .env.example .env
    echo "✓ .env file created"
else
    echo "✓ .env file exists"
fi

echo ""
echo "Setup complete! Next steps:"
echo "1. Install dependencies: pnpm install"
echo "2. Start the development server: pnpm dev"
echo "3. Open http://localhost:3000 in your browser"
echo ""
echo "Make sure the Sequencer API is running on http://localhost:9090"
