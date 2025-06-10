# Davinci Sequencer Dashboard

A web UI frontend for the Davinci Sequencer service, providing a dashboard to monitor and explore voting processes.

## Features

- **Smart Contract Links**: View and access contract addresses on the block explorer
- **Process List**: View all active processes with real-time statistics
- **Process Details**: Expandable details for each process including:
  - Vote counts and statistics
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
