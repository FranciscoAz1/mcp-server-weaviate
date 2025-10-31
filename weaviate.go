package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/weaviate/weaviate-go-client/v4/weaviate"
	"github.com/weaviate/weaviate-go-client/v4/weaviate/graphql"
	wvschema "github.com/weaviate/weaviate-go-client/v4/weaviate/schema"
	"github.com/weaviate/weaviate/entities/models"
)

type WeaviateConnection struct {
	client *weaviate.Client
}

func NewWeaviateConnection(config *Config, logger *Logger) (*WeaviateConnection, error) {
	logger.Info("Connecting to Weaviate at %s://%s", config.WeaviateScheme, config.WeaviateHost)
	client, err := weaviate.NewClient(weaviate.Config{
		Host:           config.WeaviateHost,
		Scheme:         config.WeaviateScheme,
		StartupTimeout: time.Second,
	})
	if err != nil {
		logger.Error("Failed to connect to Weaviate: %v", err)
		return nil, fmt.Errorf("connect to weaviate: %w", err)
	}
	logger.Info("Successfully connected to Weaviate")
	return &WeaviateConnection{client}, nil
}

func (conn *WeaviateConnection) InsertOne(ctx context.Context,
	collection string, props interface{},
) (*models.Object, error) {
	obj := models.Object{
		Class:      collection,
		Properties: props,
	}
	// Use batch to leverage autoschema and gRPC
	resp, err := conn.batchInsert(ctx, &obj)
	if err != nil {
		return nil, fmt.Errorf("insert one object: %w", err)
	}

	return &resp[0].Object, err
}

func (conn *WeaviateConnection) Query(ctx context.Context, collection,
	query string, targetProps []string, limit int,
) (string, error) {
	hybrid := graphql.HybridArgumentBuilder{}
	hybrid.WithQuery(query)
	builder := conn.client.GraphQL().Get().
		WithClassName(collection).WithHybrid(&hybrid).
		WithFields(func() []graphql.Field {
			fields := make([]graphql.Field, len(targetProps))
			for i, prop := range targetProps {
				fields[i] = graphql.Field{Name: prop}
			}
			return fields
		}()...)
	if limit > 0 {
		builder = builder.WithLimit(limit)
	}
	res, err := builder.Do(context.Background())
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(res)
	if err != nil {
		return "", fmt.Errorf("unmarshal query response: %w", err)
	}
	return string(b), nil
}

// Create here a function, the same as the query, but instead of using just query hybrid, use Weaviate's generate text
func (conn *WeaviateConnection) GenerateText(ctx context.Context, collection, query string, maxTokens int) (string, error) {
	// Use Weaviate's generate text feature with hybrid search + generation
	// Note: This requires Ollama to be running and accessible from Weaviate

	// Setup hybrid search
	hybrid := graphql.HybridArgumentBuilder{}
	hybrid.WithQuery(query)
	hybrid.WithTargetVectors("text")

	// Set a reasonable limit for hybrid search results
	limit := 3 // Default limit
	if maxTokens > 0 {
		// Use maxTokens as a proxy for result limit (capped at 10)
		calculatedLimit := maxTokens / 10
		if calculatedLimit < 1 {
			limit = 1
		} else if calculatedLimit > 10 {
			limit = 10
		} else {
			limit = calculatedLimit
		}
	}
	prompt := "Answer briefly: " + query
	// Try with generative search first
	generativeSearch := graphql.NewGenerativeSearch()
	generativeSearch.GroupedResult(prompt, "text")

	// Build the query with generative search
	builder := conn.client.GraphQL().Get().
		WithClassName(collection).
		WithHybrid(&hybrid).
		WithLimit(limit).
		WithGenerativeSearch(generativeSearch)

	resp, err := builder.Do(ctx)

	if err != nil {
		return "", fmt.Errorf("weaviate generate text request failed: %w", err)
	}
	b, err := json.Marshal(resp)
	if err != nil {
		return "", fmt.Errorf("unmarshal generate text response: %w", err)
	}
	return string(b), nil
}

func (conn *WeaviateConnection) GetClassSchema(ctx context.Context, className string) (*models.Class, error) {
	class, err := conn.client.Schema().ClassGetter().WithClassName(className).Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("get class schema: %w", err)
	}
	return class, nil
}

func (conn *WeaviateConnection) batchInsert(ctx context.Context, objs ...*models.Object) ([]models.ObjectsGetResponse, error) {
	resp, err := conn.client.Batch().ObjectsBatcher().WithObjects(objs...).Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("make insertion request: %w", err)
	}
	for _, res := range resp {
		if res.Result != nil && res.Result.Errors != nil && res.Result.Errors.Error != nil {
			for _, nestedErr := range res.Result.Errors.Error {
				err = errors.Join(err, errors.New(nestedErr.Message))
			}
		}
	}

	return resp, err
}

// --- Schema utilities for discovering cross-references dynamically ---

// RefInfo represents a reference property with its target classes
type RefInfo struct {
	Prop    string
	Targets []string
}

// IncomingRefInfo represents an incoming reference from another class
type IncomingRefInfo struct {
	FromClass string
	Prop      string
}

// refsForClass returns all reference properties for a given class
func (conn *WeaviateConnection) refsForClass(schemaDump *wvschema.Dump, className string) []RefInfo {
	if schemaDump == nil || schemaDump.Classes == nil {
		return []RefInfo{}
	}

	var targetClass *models.Class
	for _, cls := range schemaDump.Classes {
		if cls.Class == className {
			targetClass = cls
			break
		}
	}

	if targetClass == nil || targetClass.Properties == nil {
		return []RefInfo{}
	}

	var refs []RefInfo
	for _, prop := range targetClass.Properties {
		if prop.DataType == nil {
			continue
		}

		var targets []string
		for _, dt := range prop.DataType {
			// Check if datatype looks like a class name (starts with uppercase)
			if len(dt) > 0 && dt[0] >= 'A' && dt[0] <= 'Z' {
				targets = append(targets, dt)
			}
		}

		if len(targets) > 0 {
			refs = append(refs, RefInfo{
				Prop:    prop.Name,
				Targets: targets,
			})
		}
	}

	return refs
}

// incomingRefsForClass returns all incoming references to a given class
func (conn *WeaviateConnection) incomingRefsForClass(schemaDump *wvschema.Dump, targetClass string) []IncomingRefInfo {
	if schemaDump == nil || schemaDump.Classes == nil {
		return []IncomingRefInfo{}
	}

	var incoming []IncomingRefInfo
	for _, cls := range schemaDump.Classes {
		if cls.Properties == nil {
			continue
		}

		for _, prop := range cls.Properties {
			if prop.DataType == nil {
				continue
			}

			for _, dt := range prop.DataType {
				if dt == targetClass {
					incoming = append(incoming, IncomingRefInfo{
						FromClass: cls.Class,
						Prop:      prop.Name,
					})
					break
				}
			}
		}
	}

	return incoming
}

// refTargetsForProp returns the target classes for a specific reference property
func (conn *WeaviateConnection) refTargetsForProp(schemaDump *wvschema.Dump, className, propName string) []string {
	if schemaDump == nil || schemaDump.Classes == nil {
		return []string{}
	}

	var targetClass *models.Class
	for _, cls := range schemaDump.Classes {
		if cls.Class == className {
			targetClass = cls
			break
		}
	}

	if targetClass == nil || targetClass.Properties == nil {
		return []string{}
	}

	for _, prop := range targetClass.Properties {
		if prop.Name != propName || prop.DataType == nil {
			continue
		}

		var targets []string
		for _, dt := range prop.DataType {
			if len(dt) > 0 && dt[0] >= 'A' && dt[0] <= 'Z' {
				targets = append(targets, dt)
			}
		}
		return targets
	}

	return []string{}
}

// hasVectorizer checks if a class has a vectorizer configured
func (conn *WeaviateConnection) hasVectorizer(schemaDump *wvschema.Dump, className string) bool {
	if schemaDump == nil || schemaDump.Classes == nil {
		return false
	}

	for _, cls := range schemaDump.Classes {
		if strings.EqualFold(cls.Class, className) {
			if cls.Vectorizer != "" && strings.ToLower(cls.Vectorizer) != "none" {
				return true
			}
			if cls.ModuleConfig != nil && len(cls.ModuleConfig.(map[string]interface{})) > 0 {
				return true
			}
			return false
		}
	}

	return false
}

// QueryOrigin performs hybrid search and returns cross-reference information
func (conn *WeaviateConnection) QueryOrigin(ctx context.Context, collection, query string, limit int, targetProps []string) (string, error) {
	schema, err := conn.client.Schema().Getter().Do(ctx)
	if err != nil {
		schema = nil
	}

	useHybrid := conn.hasVectorizer(schema, collection)

	// Build fields string
	fields := make([]graphql.Field, len(targetProps))
	for i, prop := range targetProps {
		fields[i] = graphql.Field{Name: prop}
	}

	builder := conn.client.GraphQL().Get().
		WithClassName(collection).
		WithFields(fields...)

	if useHybrid {
		hybrid := graphql.HybridArgumentBuilder{}
		hybrid.WithQuery(query)
		builder = builder.WithHybrid(&hybrid)
	}

	if limit > 0 {
		builder = builder.WithLimit(limit)
	}

	result, err := builder.Do(ctx)
	if err != nil {
		return "", fmt.Errorf("query origin failed: %w", err)
	}

	// Build natural language response
	var lines []string
	lines = append(lines, fmt.Sprintf("Hybrid search results for %s (query=\"%s\", limit=%d).", collection, query, limit))

	if !useHybrid {
		lines = append(lines, fmt.Sprintf("Warning: class %s has no vectorizer configured; hybrid search was skipped.", collection))
	}

	// Get available collections
	if schema != nil && schema.Classes != nil {
		var classes []string
		for _, cls := range schema.Classes {
			classes = append(classes, cls.Class)
		}
		if len(classes) > 0 {
			lines = append(lines, fmt.Sprintf("Collections available in the schema: %s.", strings.Join(classes, ", ")))
		}
	}

	// Parse results
	data, ok := result.Data["Get"].(map[string]interface{})
	if !ok {
		lines = append(lines, fmt.Sprintf("No %s matched your query.", collection))
	} else {
		rows, ok := data[collection].([]interface{})
		if !ok || len(rows) == 0 {
			lines = append(lines, fmt.Sprintf("No %s matched your query.", collection))
		} else {
			lines = append(lines, fmt.Sprintf("Matched %d result(s):", len(rows)))
			for _, row := range rows {
				rowMap, ok := row.(map[string]interface{})
				if !ok {
					continue
				}
				// Try to get a name or string representation
				if name, ok := rowMap["name"].(string); ok {
					lines = append(lines, fmt.Sprintf("- %s", name))
				} else {
					jsonRow, _ := json.Marshal(rowMap)
					lines = append(lines, fmt.Sprintf("- %s", string(jsonRow)))
				}
			}
		}
	}

	// Discover missed fields
	if schema != nil {
		var classProps []string
		for _, cls := range schema.Classes {
			if cls.Class == collection && cls.Properties != nil {
				for _, prop := range cls.Properties {
					classProps = append(classProps, prop.Name)
				}
				break
			}
		}

		requested := make(map[string]bool)
		for _, p := range targetProps {
			requested[p] = true
		}

		var missedFields []string
		for _, p := range classProps {
			if !requested[p] {
				missedFields = append(missedFields, p)
			}
		}

		if len(missedFields) > 0 {
			show := missedFields
			if len(show) > 12 {
				show = show[:12]
			}
			suffix := ""
			if len(missedFields) > len(show) {
				suffix = ", ..."
			}
			lines = append(lines, fmt.Sprintf("Missed fields on %s: %s%s", collection, strings.Join(show, ", "), suffix))
		}
	}

	// Dynamic discovery of references
	if schema != nil {
		outgoing := conn.refsForClass(schema, collection)
		incoming := conn.incomingRefsForClass(schema, collection)

		lines = append(lines, "You can deepen the search by following these connections:")

		if len(outgoing) > 0 {
			lines = append(lines, fmt.Sprintf("From %s (outgoing refs):", collection))
			for _, ref := range outgoing {
				lines = append(lines, fmt.Sprintf("- %s -> %s", ref.Prop, strings.Join(ref.Targets, " | ")))
			}
		}

		if len(incoming) > 0 {
			lines = append(lines, fmt.Sprintf("To %s (incoming refs):", collection))
			for _, ref := range incoming {
				lines = append(lines, fmt.Sprintf("- %s.%s -> %s", ref.FromClass, ref.Prop, collection))
			}
		}
	}

	return strings.Join(lines, "\n"), nil
}

// QueryWithRefs performs hybrid search following a specific reference property
func (conn *WeaviateConnection) QueryWithRefs(ctx context.Context, collection, refProp, query string, limit int, baseProps, refProps []string) (string, error) {
	schema, err := conn.client.Schema().Getter().Do(ctx)
	if err != nil {
		schema = nil
	}

	targets := conn.refTargetsForProp(schema, collection, refProp)
	useHybrid := conn.hasVectorizer(schema, collection)

	// Build fields: base props + reference fragments for each possible target class
	fieldStrings := append([]string{}, baseProps...)

	// Add reference block
	if len(targets) > 0 {
		var fragments []string
		for _, t := range targets {
			fragmentProps := strings.Join(refProps, " ")
			fragments = append(fragments, fmt.Sprintf("... on %s { %s }", t, fragmentProps))
		}
		refBlock := fmt.Sprintf("%s { %s }", refProp, strings.Join(fragments, " "))
		fieldStrings = append(fieldStrings, refBlock)
	} else {
		// Best-effort: request the ref block without fragments
		refBlock := fmt.Sprintf("%s { %s }", refProp, strings.Join(refProps, " "))
		fieldStrings = append(fieldStrings, refBlock)
	}

	builder := conn.client.GraphQL().Get().
		WithClassName(collection).
		WithFields(graphql.Field{Name: strings.Join(fieldStrings, " ")})

	if useHybrid {
		hybrid := graphql.HybridArgumentBuilder{}
		hybrid.WithQuery(query)
		builder = builder.WithHybrid(&hybrid)
	}

	if limit > 0 {
		builder = builder.WithLimit(limit)
	}

	result, err := builder.Do(ctx)
	if err != nil {
		return "", fmt.Errorf("query with refs failed: %w", err)
	}

	// Build natural language response
	var lines []string
	lines = append(lines, fmt.Sprintf("Hybrid search for %s following reference '%s' (query=\"%s\", limit=%d).", collection, refProp, query, limit))

	if !useHybrid {
		lines = append(lines, fmt.Sprintf("Warning: class %s has no vectorizer configured; hybrid search was skipped.", collection))
	}

	// Get available collections
	if schema != nil && schema.Classes != nil {
		var classes []string
		for _, cls := range schema.Classes {
			classes = append(classes, cls.Class)
		}
		if len(classes) > 0 {
			lines = append(lines, fmt.Sprintf("Collections available: %s.", strings.Join(classes, ", ")))
		}
	}

	// Parse results
	data, ok := result.Data["Get"].(map[string]interface{})
	if !ok {
		lines = append(lines, fmt.Sprintf("No %s matched your query.", collection))
	} else {
		rows, ok := data[collection].([]interface{})
		if !ok || len(rows) == 0 {
			lines = append(lines, fmt.Sprintf("No %s matched your query.", collection))
		} else {
			lines = append(lines, fmt.Sprintf("Matched %d result(s) with their references:", len(rows)))
			for _, row := range rows {
				rowMap, ok := row.(map[string]interface{})
				if !ok {
					continue
				}

				// Get base name
				baseName := "unknown"
				if name, ok := rowMap["name"].(string); ok {
					baseName = name
				}

				// Get referenced items
				var refNames []string
				if refArr, ok := rowMap[refProp].([]interface{}); ok {
					for _, refItem := range refArr {
						if refMap, ok := refItem.(map[string]interface{}); ok {
							if refName, ok := refMap["name"].(string); ok {
								refNames = append(refNames, refName)
							}
						}
					}
				}

				refText := "none"
				if len(refNames) > 0 {
					refText = strings.Join(refNames, ", ")
				}

				lines = append(lines, fmt.Sprintf("- %s: %s -> [%s]", collection, baseName, refText))
			}
		}
	}

	// Discover missed fields on base collection
	if schema != nil {
		var classProps []string
		for _, cls := range schema.Classes {
			if cls.Class == collection && cls.Properties != nil {
				for _, prop := range cls.Properties {
					classProps = append(classProps, prop.Name)
				}
				break
			}
		}

		requested := make(map[string]bool)
		for _, p := range baseProps {
			requested[p] = true
		}

		var missedFields []string
		for _, p := range classProps {
			if !requested[p] {
				missedFields = append(missedFields, p)
			}
		}

		if len(missedFields) > 0 {
			show := missedFields
			if len(show) > 12 {
				show = show[:12]
			}
			suffix := ""
			if len(missedFields) > len(show) {
				suffix = ", ..."
			}
			lines = append(lines, fmt.Sprintf("Missed fields on %s: %s%s", collection, strings.Join(show, ", "), suffix))
		}
	}

	// Show connections for further exploration
	if schema != nil {
		outgoing := conn.refsForClass(schema, collection)
		incoming := conn.incomingRefsForClass(schema, collection)

		lines = append(lines, "You can deepen the search by following these connections:")

		if len(outgoing) > 0 {
			lines = append(lines, fmt.Sprintf("From %s (outgoing refs):", collection))
			for _, ref := range outgoing {
				lines = append(lines, fmt.Sprintf("- %s -> %s", ref.Prop, strings.Join(ref.Targets, " | ")))
			}
		}

		if len(incoming) > 0 {
			lines = append(lines, fmt.Sprintf("To %s (incoming refs):", collection))
			for _, ref := range incoming {
				lines = append(lines, fmt.Sprintf("- %s.%s -> %s", ref.FromClass, ref.Prop, collection))
			}
		}
	}

	return strings.Join(lines, "\n"), nil
}
