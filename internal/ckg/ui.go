package ckg

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

//go:embed ui/index.html
var uiFS embed.FS

type GraphData struct {
	Nodes []GraphNode `json:"nodes"`
	Links []GraphLink `json:"links"`
}

type GraphNode struct {
	ID    string         `json:"id"`
	Group string         `json:"group"` // file, folder, struct, interface, func, method, test, global_var
	Name  string         `json:"name"`
	Meta  map[string]any `json:"meta,omitempty"` // Rich metadata for the Bottom Panel
}

type GraphLink struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	Relation string `json:"relation"` // in_file, calls, uses
}

func sourceHandlerFunc(workspaceRoot string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filePath := r.URL.Query().Get("file")
		startStr := r.URL.Query().Get("start")
		endStr := r.URL.Query().Get("end")
		if filePath == "" || startStr == "" || endStr == "" {
			http.Error(w, "missing params", http.StatusBadRequest)
			return
		}
		fullPath := filepath.Join(workspaceRoot, filepath.FromSlash(filePath))
		rel, relErr := filepath.Rel(workspaceRoot, fullPath)
		if relErr != nil || strings.HasPrefix(rel, "..") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		content, err := os.ReadFile(fullPath)
		if err != nil {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		lines := strings.Split(string(content), "\n")
		start := 0
		end := len(lines)
		fmt.Sscanf(startStr, "%d", &start)
		fmt.Sscanf(endStr, "%d", &end)
		if start < 1 {
			start = 1
		}
		if end > len(lines) {
			end = len(lines)
		}
		snippet := strings.Join(lines[start-1:end], "\n")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(snippet))
	}
}

// StartUIServer starts a lightweight HTTP server on the given port to serve the CKG visualization.
func StartUIServer(store *Store, workspaceRoot string, port int) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		data, err := buildGraphData(ctx, store)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	})

	// Serve source code snippets
	mux.HandleFunc("/api/source", sourceHandlerFunc(workspaceRoot))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		indexHtml, err := uiFS.ReadFile("ui/index.html")
		if err != nil {
			http.Error(w, "UI not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHtml)
	})

	addr := fmt.Sprintf(":%d", port)
	log.Printf("Starting CKG UI server at http://localhost%s", addr)
	return http.ListenAndServe(addr, mux)
}

func buildGraphData(ctx context.Context, store *Store) (*GraphData, error) {
	data := &GraphData{
		Nodes: []GraphNode{},
		Links: []GraphLink{},
	}

	// 1. Fetch Files
	fileRows, err := store.db.QueryContext(ctx, "SELECT id, path, language FROM files")
	if err != nil {
		return nil, err
	}
	defer fileRows.Close()

	fileIDtoPath := make(map[int64]string)
	fileStats := make(map[string]int) // path -> symbol count

	for fileRows.Next() {
		var id int64
		var filePath, lang string
		if err := fileRows.Scan(&id, &filePath, &lang); err == nil {
			fileIDtoPath[id] = filePath
			
			// Skip testdata and vendor from graph visualization
			if strings.HasPrefix(filePath, "testdata") || strings.HasPrefix(filePath, "vendor") {
				continue
			}
			// We will update 'Символов внутри' later
			data.Nodes = append(data.Nodes, GraphNode{
				ID:    filePath,
				Group: "file",
				Name:  path.Base(filePath),
				Meta: map[string]any{
					"Файл": filePath,
					"Язык": lang,
				},
			})
		}
	}

	// 2. Fetch Nodes (v2 schema: fqn, short_name, kind)
	nodeRows, err := store.db.QueryContext(ctx, "SELECT id, file_id, fqn, short_name, kind, line_start, line_end FROM nodes")
	if err != nil {
		return nil, err
	}
	defer nodeRows.Close()

	nodeIdToGlobalId := make(map[int64]string)
	// fqnToGlobalId maps FQN → graph node ID for edge resolution
	fqnToGlobalId := make(map[string]string)

	// Track connections for metadata
	callsCount := make(map[string]int)
	calledByCount := make(map[string]int)

	for nodeRows.Next() {
		var id, fileID int64
		var fqn, shortName, kind string
		var start, end int
		if err := nodeRows.Scan(&id, &fileID, &fqn, &shortName, &kind, &start, &end); err == nil {
			filePath := fileIDtoPath[fileID]

			// Skip nodes from testdata/vendor
			if strings.HasPrefix(filePath, "testdata") || strings.HasPrefix(filePath, "vendor") {
				continue
			}
			fileStats[filePath]++

			// Map v2 kind → UI group. Keep "func" as default.
			group := "func"
			switch kind {
			case "method":
				group = "method"
			case "struct":
				group = "struct"
			case "interface":
				group = "interface"
			case "type":
				group = "struct" // render generic types as struct
			}

			// Simple heuristic: if short_name starts with Test, it's a test
			if strings.HasPrefix(shortName, "Test") {
				group = "test"
			}

			// Use FQN as the unique graph ID; short_name as the display label
			globalID := fqn
			nodeIdToGlobalId[id] = globalID
			fqnToGlobalId[fqn] = globalID

			meta := map[string]any{
				"Имя":   shortName,
				"FQN":   fqn,
				"Файл":  filePath,
				"Строки": fmt.Sprintf("%d-%d", start, end),
			}

			data.Nodes = append(data.Nodes, GraphNode{
				ID:    globalID,
				Group: group,
				Name:  shortName, // JSON "name" backed by short_name
				Meta:  meta,
			})

			data.Links = append(data.Links, GraphLink{
				Source:   globalID,
				Target:   filePath,
				Relation: "in_file",
			})
		}
	}

	// Update file symbol counts
	for i, n := range data.Nodes {
		if n.Group == "file" {
			data.Nodes[i].Meta["Символов внутри"] = fileStats[n.ID]
		}
	}

	// 3. Fetch Edges (v2 schema: source_id, target_id, target_fqn, relation)
	// source_id and target_id are FKs to nodes.id; target_fqn is always populated
	// (target_id may be NULL for external/unresolved symbols).
	edgeRows, err := store.db.QueryContext(ctx, `
		SELECT e.source_id, e.target_id, e.target_fqn, e.relation
		FROM edges e
	`)
	if err != nil {
		return nil, err
	}
	defer edgeRows.Close()

	for edgeRows.Next() {
		var sourceID int64
		var targetIDPtr *int64
		var targetFQN, relation string
		if err := edgeRows.Scan(&sourceID, &targetIDPtr, &targetFQN, &relation); err == nil {
			sourceGlobalID, ok := nodeIdToGlobalId[sourceID]
			if !ok {
				// Source node was skipped (testdata/vendor) or not yet indexed
				continue
			}

			// Prefer target_id lookup (internal node); fall back to target_fqn as label
			var targetGlobalID string
			if targetIDPtr != nil {
				if gid, ok := nodeIdToGlobalId[*targetIDPtr]; ok {
					targetGlobalID = gid
				}
			}
			if targetGlobalID == "" {
				// External symbol: use target_fqn as the graph node ID.
				// If the FQN happens to match an indexed node, use that.
				if gid, ok := fqnToGlobalId[targetFQN]; ok {
					targetGlobalID = gid
				} else {
					targetGlobalID = targetFQN
				}
			}

			data.Links = append(data.Links, GraphLink{
				Source:   sourceGlobalID,
				Target:   targetGlobalID,
				Relation: relation,
			})

			callsCount[sourceGlobalID]++
			calledByCount[targetGlobalID]++
		}
	}

	// Add counts to node metadata
	for i, n := range data.Nodes {
		if n.Group == "func" || n.Group == "method" {
			data.Nodes[i].Meta["Вызовов (исходящих)"] = callsCount[n.ID]
			data.Nodes[i].Meta["Вызовов (входящих)"] = calledByCount[n.ID]
		}
	}

	// Build folder hierarchy (use path.Dir for consistent forward slashes)
	folders := make(map[string]bool)
	folderFilesCount := make(map[string]int) // direct children only
	for _, p := range fileIDtoPath {
		if strings.HasPrefix(p, "testdata") || strings.HasPrefix(p, "vendor") {
			continue
		}
		// Count only direct parent folder
		directParent := path.Dir(p)
		if directParent != "." && directParent != "/" {
			folderFilesCount[directParent]++
		}
		// Build full hierarchy
		dir := path.Dir(p)
		for dir != "." && dir != "/" {
			folders[dir] = true
			dir = path.Dir(dir)
		}
	}

	for f := range folders {
		data.Nodes = append(data.Nodes, GraphNode{
			ID:    f,
			Group: "folder",
			Name:  path.Base(f),
			Meta: map[string]any{
				"Путь":          f,
				"Файлов внутри": folderFilesCount[f],
			},
		})
	}

	return data, nil
}
