# Weaviate MCP Server

A Model Context Protocol (MCP) server for interacting with Weaviate vector databases.

## Features

- **Insert Objects**: Add data to Weaviate collections
- **Hybrid Search**: Query Weaviate using natural language with hybrid search
- **Configurable**: Environment variables and command-line options
- **Logging**: Structured logging with multiple output options
- **Security**: Read-only mode and tool disabling
- **Cross-platform**: Works on Windows, macOS, and Linux

## Prerequisites

- Go 1.23.1 or later
- Weaviate instance (local or cloud)

## Installation

### From Source

```bash
git clone https://github.com/weaviate/mcpviate.git
cd mcp-server-weaviate
go build -o mcp-server .
```

### Docker

```bash
docker build -t weaviate-mcp-server .
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `WEAVIATE_HOST` | `host.docker.internal:8080` | Weaviate server host |
| `WEAVIATE_SCHEME` | `http` | Weaviate connection scheme |
| `MCP_TRANSPORT` | `stdio` | Transport protocol (`stdio` or `http`) |
| `MCP_HTTP_PORT` | `3000` | HTTP port when using HTTP transport |
| `MCP_HTTP_HOST` | `127.0.0.1` | HTTP host when using HTTP transport |
| `MCP_LOG_LEVEL` | `info` | Log level (`debug`, `info`, `warn`, `error`) |
| `MCP_LOG_OUTPUT` | `stderr` | Log output (`stderr`, `file`, `both`) |
| `MCP_READ_ONLY` | `false` | Enable read-only mode |
| `MCP_DISABLED_TOOLS` | (none) | Comma-separated list of disabled tools |
| `MCP_DEFAULT_COLLECTION` | `DefaultCollection` | Default collection name |

### Command-Line Flags

```bash
./mcp-server --help
```

Available flags:
- `--weaviate-host`: Weaviate host
- `--weaviate-scheme`: Weaviate scheme
- `--transport`: Transport protocol
- `--http-port`: HTTP port
- `--http-host`: HTTP host
- `--log-level`: Log level
- `--log-output`: Log output
- `--read-only`: Enable read-only mode
- `--default-collection`: Default collection name

## Setup

### Local Development

1. **Start Weaviate**:
```bash
docker-compose up -d
```

2. **Build the server**:
```bash
make build
```

3. **Run the test client**:
```bash
make run-client
```

### Docker Setup

1. **Start Weaviate stack**:
```bash
docker-compose up -d
```

2. **Run MCP server in Docker**:
```bash
docker run --rm -i \
  -e WEAVIATE_HOST=host.docker.internal:8080 \
  -e WEAVIATE_SCHEME=http \
  --network host \
  weaviate-mcp-server
```

### MCP Client Configuration

Add to your MCP client configuration (e.g., `claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "weaviate": {
      "command": "/path/to/mcp-server",
      "env": {
        "WEAVIATE_HOST": "localhost:8080",
        "WEAVIATE_SCHEME": "http"
      }
    }
  }
}
```

## Tools

### weaviate-insert-one

Insert an object into a Weaviate collection.

**Parameters:**
- `collection` (string, optional): Target collection name
- `properties` (object, required): Object properties to insert

**Example:**
```json
{
  "collection": "WorldMap",
  "properties": {
    "continent": "Europe",
    "country": "Spain",
    "city": "Valencia"
  }
}
```

### weaviate-query

Query objects from a Weaviate collection using hybrid search.

**Parameters:**
- `query` (string, required): Natural language query
- `targetProperties` (array of strings, required): Properties to return
- `collection` (string, optional): Target collection name

**Example:**
```json
{
  "query": "What country is Valencia in?",
  "targetProperties": ["continent", "country", "city"],
  "collection": "WorldMap"
}
```

**Note:** `targetProperties` must contain at least one property name. Empty arrays will result in an error.

## Prompts

The server includes example prompts for testing the tools. See [`prompts.md`](prompts.md) for ready-to-use prompt templates that demonstrate how to use the insert and query tools.

The prompts include:
- **Dataset-specific prompts** for LiHua-World data (dinner events, travel stories, personal events, relationships)
- **General testing prompts** for basic tool functionality
- **Custom search prompts** for flexible querying

These prompts can be copied into MCP clients that support custom prompts (like Claude Desktop) to provide guided interactions with the Weaviate database.

## Testing

### MCP Inspector (Recommended)

Use the MCP Inspector for debugging and testing:

```bash
npm install -g @modelcontextprotocol/inspector
mcp-inspector --command "./mcp-server"
```

This provides a web interface to interact with your MCP server.

### Test Client

The included test client demonstrates basic usage:

```bash
make run-client
```

### Manual Testing with curl (HTTP Transport)

If using HTTP transport:

```bash
# Initialize
curl -X POST http://localhost:3000/jsonrpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {}}'

# List tools
curl -X POST http://localhost:3000/jsonrpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}}'

# Call tool
curl -X POST http://localhost:3000/jsonrpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "weaviate-query", "arguments": {"query": "test", "targetProperties": ["field"]}}}'
```

## Troubleshooting

### Common Issues

1. **Connection Errors**: Check Weaviate is running and accessible
2. **Tool Not Found**: Verify tool isn't disabled and read-only mode isn't active
3. **Logging Issues**: Check log output configuration

### Debug Mode

Enable debug logging:

```bash
export MCP_LOG_LEVEL=debug
./mcp-server
```

### Log Files

When using file logging, logs are written to `logs/mcp-server.log`.

## Registry Submission

To submit to the MCP registry, use the provided `server.yaml` file.

## License

MIT
