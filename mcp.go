package main

import (
	"context"
	"log"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type MCPServer struct {
	server            *server.MCPServer
	weaviateConn      *WeaviateConnection
	defaultCollection string
}

func NewMCPServer() (*MCPServer, error) {
	conn, err := NewWeaviateConnection()
	if err != nil {
		return nil, err
	}
	s := &MCPServer{
		server: server.NewMCPServer(
			"Weaviate MCP Server",
			"0.1.0",
			server.WithToolCapabilities(true),
			server.WithPromptCapabilities(true),
			server.WithResourceCapabilities(true, true),
			server.WithRecovery(),
		),
		weaviateConn: conn,
		// TODO: configurable collection name
		defaultCollection: "DefaultCollection",
	}
	s.registerTools()
	return s, nil
}

func (s *MCPServer) Serve() {
	log.Println("MCPServer starting: waiting for requests...")
	server.ServeStdio(s.server)
}

func (s *MCPServer) registerTools() {
	insertOne := mcp.NewTool(
		"weaviate-insert-one",
		mcp.WithString(
			"collection",
			mcp.Description("Name of the target collection"),
		),
		mcp.WithObject(
			"properties",
			mcp.Description("Object properties to insert"),
			mcp.Required(),
		),
	)
	query := mcp.NewTool(
		"weaviate-query",
		mcp.WithString(
			"query",
			mcp.Description("Query data within Weaviate"),
			mcp.Required(),
		),
		mcp.WithArray(
			"targetProperties",
			mcp.Description("Properties to return with the query"),
			mcp.Required(),
		),
	)

	s.server.AddTools(
		server.ServerTool{Tool: insertOne, Handler: s.weaviateInsertOne},
		server.ServerTool{Tool: query, Handler: s.weaviateQuery},
	)
}

func (s *MCPServer) weaviateInsertOne(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	log.Printf("InsertOne called: collection=%v, args=%v", req.Params.Arguments["collection"], req.Params.Arguments)
	targetCol := s.parseTargetCollection(req)
	propsRaw, ok := req.Params.Arguments["properties"]
	if !ok {
		log.Println("Missing 'properties' argument")
		return mcp.NewToolResultError("Missing 'properties' argument"), nil
	}
	props, ok := propsRaw.(map[string]interface{})
	if !ok {
		log.Printf("'properties' argument is not a map: %T", propsRaw)
		return mcp.NewToolResultError("'properties' argument must be an object"), nil
	}
	res, err := s.weaviateConn.InsertOne(context.Background(), targetCol, props)
	if err != nil {
		log.Printf("InsertOne error: %v", err)
		return mcp.NewToolResultErrorFromErr("failed to insert object", err), nil
	}
	log.Printf("InsertOne success: id=%v", res.ID.String())
	return mcp.NewToolResultText(res.ID.String()), nil
}

func (s *MCPServer) weaviateQuery(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	log.Printf("Query called: collection=%v, args=%v", req.Params.Arguments["collection"], req.Params.Arguments)
	targetCol := s.parseTargetCollection(req)
	queryRaw, ok := req.Params.Arguments["query"]
	if !ok {
		log.Println("Missing 'query' argument")
		return mcp.NewToolResultError("Missing 'query' argument"), nil
	}
	query, ok := queryRaw.(string)
	if !ok {
		log.Printf("'query' argument is not a string: %T", queryRaw)
		return mcp.NewToolResultError("'query' argument must be a string"), nil
	}
	propsRaw, ok := req.Params.Arguments["targetProperties"]
	if !ok {
		log.Println("Missing 'targetProperties' argument")
		return mcp.NewToolResultError("Missing 'targetProperties' argument"), nil
	}
	props, ok := propsRaw.([]interface{})
	if !ok {
		log.Printf("'targetProperties' argument is not an array: %T", propsRaw)
		return mcp.NewToolResultError("'targetProperties' argument must be an array"), nil
	}
	var targetProps []string
	for _, prop := range props {
		typed, ok := prop.(string)
		if !ok {
			log.Printf("targetProperties contains non-string: %v (%T)", prop, prop)
			return mcp.NewToolResultError("targetProperties must contain only strings"), nil
		}
		targetProps = append(targetProps, typed)
	}
	res, err := s.weaviateConn.Query(context.Background(), targetCol, query, targetProps)
	if err != nil {
		log.Printf("Query error: %v", err)
		return mcp.NewToolResultErrorFromErr("failed to process query", err), nil
	}
	log.Printf("Query success: result=%v", res)
	return mcp.NewToolResultText(res), nil
}

func (s *MCPServer) parseTargetCollection(req mcp.CallToolRequest) string {
	var (
		targetCol = s.defaultCollection
	)
	col, ok := req.Params.Arguments["collection"].(string)
	if ok {
		targetCol = col
	}
	return targetCol
}
