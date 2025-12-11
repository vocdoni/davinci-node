# Davinci Sequencer Dashboard

A web UI frontend for the Davinci Sequencer service, providing a dashboard to monitor and explore voting processes.

## Features

- **Smart Contract Links**: View and access contract addresses on the block explorer
- **Process List**: View all active processes with real-time statistics
- **Process Details**: Expandable details for each process including:
  - Voters count and statistics
  - Sequencer performance metrics
  - State transition information
  - Census details
- **Auto-refresh**: Dashboard updates every 10 seconds
- **Status Filtering**: Filter processes by status (Ready, Ended, Canceled, etc.)

## Prerequisites

- Node.js (v16 or higher)
- pnpm package manager
- Davinci Sequencer API running on localhost:9090

## Installation

1. Navigate to the webapp directory:
```bash
cd webapp
```

2. Install dependencies:
```bash
pnpm install
```

3. Copy the environment variables file:
```bash
cp .env.example .env
```

4. (Optional) Edit `.env` to customize:
   - `SEQUENCER_API_URL`: The URL of the Sequencer API (default: http://localhost:9090)
   - `BLOCK_EXPLORER_URL`: The block explorer URL pattern (default: https://sepolia.etherscan.io/address)

## Development

Start the development server:
```bash
pnpm dev
```

The application will be available at http://localhost:3000

## Build

Build for production:
```bash
pnpm build
```

The built files will be in the `dist` directory.

## Technology Stack

- **React 18** with TypeScript
- **Vite** for fast development and building
- **Chakra UI** for component library
- **TanStack Query** for data fetching and caching
- **React Router** for navigation

## Project Structure

```
webapp/
├── src/
│   ├── components/      # Reusable UI components
│   ├── hooks/          # Custom React hooks
│   ├── pages/          # Page components
│   ├── router/         # Routing configuration
│   ├── themes/         # Chakra UI theme customization
│   └── types/          # TypeScript type definitions
├── index.html          # Entry HTML file
├── package.json        # Dependencies and scripts
├── tsconfig.json       # TypeScript configuration
└── vite.config.ts      # Vite configuration
```

## Configuration Options

The webapp supports multiple ways to configure the Sequencer API URL:

### 1. Environment Variables (Docker)
When running with Docker, set environment variables:
```bash
# Via docker-compose
SEQUENCER_API_URL=http://my-sequencer:9090 docker-compose up

# Or in .env file
SEQUENCER_API_URL=http://my-sequencer:9090
BLOCK_EXPLORER_URL=https://etherscan.io/address
```

### 2. Environment Variables (Development)
For local development, create a `.env` file:
```bash
SEQUENCER_API_URL=http://localhost:9090
BLOCK_EXPLORER_URL=https://sepolia.etherscan.io/address
```

### 3. In-App Configuration
Use the input field at the top of the dashboard to change the API URL on the fly.

### Priority Order
1. Runtime configuration (Docker environment variables)
2. Build-time environment variables (.env file during build)
3. Default: `http://localhost:9090`

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `SEQUENCER_API_URL` | URL of the Sequencer API | `http://localhost:9090` |
| `BLOCK_EXPLORER_URL` | Block explorer URL pattern | `https://sepolia.etherscan.io/address` |

## API Integration

The dashboard connects to the Sequencer API endpoints:
- `GET /info` - Retrieves contract addresses and system information
- `GET /processes` - Lists all process IDs
- `GET /processes/{processId}` - Gets detailed information for a specific process

## Contributing

When making changes:
1. Follow the existing code style and patterns
2. Use TypeScript for type safety
3. Test thoroughly with the Sequencer API
4. Update this README if adding new features
