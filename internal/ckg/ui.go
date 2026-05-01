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
	mux.HandleFunc("/api/source", func(w http.ResponseWriter, r *http.Request) {
		filePath := r.URL.Query().Get("file")
		startStr := r.URL.Query().Get("start")
		endStr := r.URL.Query().Get("end")
		if filePath == "" || startStr == "" || endStr == "" {
			http.Error(w, "missing params", http.StatusBadRequest)
			return
		}
		// Normalize forward slashes to OS separators for filepath.Join
		filePath = strings.ReplaceAll(filePath, "/", string(filepath.Separator))
		// Security: prevent path traversal
		if strings.Contains(filePath, "..") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		fullPath := filepath.Join(workspaceRoot, filePath)
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
		if start < 1 { start = 1 }
		if end > len(lines) { end = len(lines) }
		snippet := strings.Join(lines[start-1:end], "\n")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(snippet))
	})

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

	// 2. Fetch Nodes
	nodeRows, err := store.db.QueryContext(ctx, "SELECT id, file_id, name, type, line_start, line_end FROM nodes")
	if err != nil {
		return nil, err
	}
	defer nodeRows.Close()

	nodeIdToGlobalId := make(map[int64]string)
	
	// Track connections for metadata
	callsCount := make(map[string]int)
	calledByCount := make(map[string]int)

	for nodeRows.Next() {
		var id, fileID int64
		var name, nodeType string
		var start, end int
		if err := nodeRows.Scan(&id, &fileID, &name, &nodeType, &start, &end); err == nil {
			filePath := fileIDtoPath[fileID]
			
			// Skip nodes from testdata/vendor
			if strings.HasPrefix(filePath, "testdata") || strings.HasPrefix(filePath, "vendor") {
				continue
			}
			fileStats[filePath]++

			group := "func"
			switch nodeType {
			case "method_declaration":
				group = "method"
			case "type_spec":
				// Could be struct or interface, map to struct for now
				group = "struct" 
			}

			// Simple heuristic: if name starts with Test, it's a test
			if strings.HasPrefix(name, "Test") {
				group = "test"
			}

			globalID := fmt.Sprintf("%s:%s", filePath, name)
			nodeIdToGlobalId[id] = globalID

			meta := map[string]any{
				"Имя": name,
				"Файл": filePath,
				"Строки": fmt.Sprintf("%d-%d", start, end),
			}

			data.Nodes = append(data.Nodes, GraphNode{
				ID:    globalID,
				Group: group,
				Name:  name,
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

	// 3. Batch-load node name → file path mapping (eliminates N+1 queries)
	nodeNameToFile := make(map[string]string)
	nameRows, err := store.db.QueryContext(ctx, "SELECT n.name, f.path FROM nodes n JOIN files f ON n.file_id = f.id")
	if err != nil {
		return nil, err
	}
	defer nameRows.Close()
	for nameRows.Next() {
		var name, fp string
		if err := nameRows.Scan(&name, &fp); err == nil {
			nodeNameToFile[name] = fp
		}
	}

	// 4. Fetch Edges (no more per-row queries)
	edgeRows, err := store.db.QueryContext(ctx, `
		SELECT f.path, e.source_name, e.target_name, e.relation
		FROM edges e
		JOIN files f ON e.file_id = f.id
	`)
	if err != nil {
		return nil, err
	}
	defer edgeRows.Close()

	for edgeRows.Next() {
		var filePath, sourceName, targetName, relation string
		if err := edgeRows.Scan(&filePath, &sourceName, &targetName, &relation); err == nil {
			sourceGlobalID := fmt.Sprintf("%s:%s", filePath, sourceName)

			// O(1) lookup instead of SQL query per edge
			targetGlobalID := targetName
			if tfp, ok := nodeNameToFile[targetName]; ok {
				targetGlobalID = fmt.Sprintf("%s:%s", tfp, targetName)
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
