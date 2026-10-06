package admin

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func mcpCatalogSession(t *testing.T, p mcpPrincipal) *mcp.ClientSession {
	t.Helper()
	ct, st := mcp.NewInMemoryTransports()
	server, err := newMCPServer(p).Connect(t.Context(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	client, err := mcp.NewClient(&mcp.Implementation{Name: "catalog-test", Version: "1"}, nil).Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func mcpCatalogCall(t *testing.T, session *mcp.ClientSession, args any) (map[string]any, []mcpInterfaceDoc) {
	t.Helper()
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "mcp_list", Arguments: args})
	if err != nil || res.IsError {
		t.Fatalf("mcp_list: %v, %+v", err, res)
	}
	var result struct {
		Version    string            `json:"version"`
		Interfaces []mcpInterfaceDoc `json:"interfaces"`
	}
	raw := []byte(res.Content[0].(*mcp.TextContent).Text)
	if err := json.Unmarshal(raw, &result); err != nil || result.Version != mcpInterfaceVersion {
		t.Fatalf("invalid catalog: %v, %s", err, raw)
	}
	var object map[string]any
	_ = json.Unmarshal(raw, &object)
	structured, _ := json.Marshal(res.StructuredContent)
	var structuredObject map[string]any
	_ = json.Unmarshal(structured, &structuredObject)
	if !reflect.DeepEqual(object, structuredObject) {
		t.Fatal("text and structured catalog differ")
	}
	return object, result.Interfaces
}

func validateMCPSchema(t *testing.T, schema, value any) {
	t.Helper()
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	resolved, err := s.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(value)
	var data any
	_ = json.Unmarshal(raw, &data)
	if err := resolved.Validate(data); err != nil {
		t.Fatalf("schema mismatch: %v; value=%s", err, raw)
	}
}

func TestMCPCatalogScopesAndSchemas(t *testing.T) {
	for _, tc := range []struct {
		name      string
		principal mcpPrincipal
		count     int
	}{
		{"read", mcpPrincipal{}, 14},
		{"prepare", mcpPrincipal{CanPrepare: true}, 16},
		{"execute", mcpPrincipal{CanPrepare: true, CanExecute: true}, 18},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := mcpCatalogSession(t, tc.principal)
			list, err := session.ListTools(t.Context(), nil)
			if err != nil || len(list.Tools) != tc.count {
				t.Fatalf("tools/list: %v, %+v", err, list)
			}
			catalog, docs := mcpCatalogCall(t, session, map[string]any{})
			if len(docs) != len(list.Tools) {
				t.Fatal("catalog differs from tools/list")
			}
			byName := map[string]mcpInterfaceDoc{}
			for _, doc := range docs {
				if _, exists := byName[doc.Name]; exists {
					t.Fatal("duplicate interface", doc.Name)
				}
				byName[doc.Name] = doc
			}
			for _, tool := range list.Tools {
				doc, ok := byName[tool.Name]
				if !ok || doc.Description != tool.Description || len(doc.Output.Fields) == 0 || doc.SideEffects == "" {
					t.Fatalf("incomplete documentation for %s", tool.Name)
				}
				for _, param := range doc.Params {
					if param.Description == "" {
						t.Fatalf("missing description for %s.%s", tool.Name, param.Name)
					}
				}
				validateMCPSchema(t, tool.InputSchema, doc.InputExample)
				validateMCPSchema(t, tool.OutputSchema, doc.Output.Example)
				if tool.Annotations == nil || tool.Annotations.DestructiveHint == nil {
					t.Fatal("missing annotations", tool.Name)
				}
				if tool.Name == "mcp_list" {
					validateMCPSchema(t, tool.OutputSchema, catalog)
				}
				if tc.name == "read" && !tool.Annotations.ReadOnlyHint {
					t.Fatal("read scope exposed a write tool", tool.Name)
				}
			}
			for _, pair := range [][2]string{{"list_resources", "resources_list"}, {"get_resource", "resource_get"}, {"list_tags", "tags_list"}, {"read_material", "material_read"}, {"read_url", "url_read"}, {"get_action", "action_get"}} {
				old, canonical := byName[pair[0]], byName[pair[1]]
				if !old.Deprecated || canonical.Deprecated || old.CanonicalName != pair[1] || !reflect.DeepEqual(old.Params, canonical.Params) || !reflect.DeepEqual(old.Annotations, canonical.Annotations) {
					t.Fatalf("alias mismatch: %v", pair)
				}
			}
		})
	}
}

func TestMCPCatalogFilterAndWriteIsolation(t *testing.T) {
	session := mcpCatalogSession(t, mcpPrincipal{})
	_, all := mcpCatalogCall(t, session, nil)
	if len(all) != 14 {
		t.Fatal("mcp_list without arguments did not return all visible tools")
	}
	for _, name := range []string{"resources_list", "list_resources", "mcp_list"} {
		_, docs := mcpCatalogCall(t, session, map[string]any{"tool": name})
		if len(docs) != 1 || docs[0].Name != name {
			t.Fatalf("filter ignored: %s", name)
		}
	}
	for _, name := range []string{"action_prepare", "prepare_action", "action_execute", "execute_action", "not_a_tool"} {
		res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "mcp_list", Arguments: map[string]any{"tool": name}})
		if err != nil || !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, `"error"`) {
			t.Fatalf("unavailable tool documented: %s %v %+v", name, err, res)
		}
		res, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err == nil && !res.IsError {
			t.Fatal("read-only scope invoked unavailable tool", name)
		}
	}
	for _, name := range []string{"read_url", "url_read"} {
		res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{"url": "http://127.0.0.1/private"}})
		if err != nil || !res.IsError {
			t.Fatalf("private URL accepted by %s: %v %+v", name, err, res)
		}
	}
}

func TestMCPLegacyMaterialOutput(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		res, err := mcpOutput(json.RawMessage(`[{"title":"example"}]`), legacy)
		if err != nil {
			t.Fatal(err)
		}
		text := res.Content[0].(*mcp.TextContent).Text
		if strings.HasPrefix(text, "[") != legacy {
			t.Fatalf("legacy array compatibility changed: %s", text)
		}
		validateMCPSchema(t, mcpToolMetadata("read_material").OutputSchema, res.StructuredContent)
	}
}

func TestMCPGatewayErrorsPreserveInternalContract(t *testing.T) {
	for _, err := range []error{apperr.EditConflict("版本冲突"), errors.New("secret-token /private/path stack trace")} {
		text := encodedMCPError(err)
		var result map[string]any
		if json.Unmarshal([]byte(text), &result) != nil || result["error"] != result["message"] || result["code"] == nil {
			t.Fatal("missing gateway or legacy error fields", text)
		}
		if strings.Contains(text, "secret-token") || strings.Contains(text, "/private/path") {
			t.Fatal("internal error leaked", text)
		}
		var decoded *apperr.Error
		if !errors.As(decodedMCPError(text), &decoded) || decoded.Code != result["code"] {
			t.Fatal("legacy error decoding changed")
		}
	}
}

// Optional artifact for running the gateway's actual compliance checker without production access.
func TestMCPExportGatewayContract(t *testing.T) {
	path := os.Getenv("NEX_MCP_CONTRACT_FILE")
	if path == "" {
		t.Skip("NEX_MCP_CONTRACT_FILE is not set")
	}
	session := mcpCatalogSession(t, mcpPrincipal{})
	tools, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog, _ := mcpCatalogCall(t, session, map[string]any{})
	raw, err := json.MarshalIndent(map[string]any{"tools": tools.Tools, "mcpList": catalog}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
