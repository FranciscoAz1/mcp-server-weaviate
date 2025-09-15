# Weaviate MCP Server

## Instructions

### Local Development

Build the server:
```
make build
```

Run the test client
```
make run-client
```

### Docker Setup

To run the full stack with Weaviate:

1. Start Weaviate and dependencies:
```bash
docker-compose up -d
```

2. Run the MCP server:
```bash
docker run --rm -i -e WEAVIATE_HOST=host.docker.internal:8080 -e WEAVIATE_SCHEME=http --network host cr.weaviate.io/semitechnologies/weaviate:1.32.4
```

## Tools

### Insert One
Insert an object into weaviate.

**Request body:**
```json
{}
```

**Response body**
```json
{}
```

### Query
Retrieve objects from weaviate with hybrid search.

**Request body:**
```json
{}
```

**Response body**
```json
{}
```
