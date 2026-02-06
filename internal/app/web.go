package app

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
)

type webServer struct {
	repo      *Repository
	templates *template.Template
}

type dashboardData struct {
	RepoRoot            string
	CurrentWorkspace    string
	Workspaces          []Workspace
	Types               []typeData
	WorkspaceDirtyFiles []string
	Flash               string
	FlashError          bool
	ValidationRan       bool
	ValidationOK        bool
	ValidationIssues    []ValidationIssue
}

type typeData struct {
	Name    string
	Objects []Object
}

type objectData struct {
	Workspace   string
	Type        string
	ID          string
	ReadOnly    bool
	Fields      []fieldData
	FieldValues map[string]string
	Diffs       []fieldDiff
	Flash       string
	FlashError  bool
}

type fieldData struct {
	Name      string
	Type      string
	ItemsType string
	Required  bool
	Enum      []string
}

type fieldDiff struct {
	Field     string
	Main      string
	Workspace string
	Status    string
}

type conflictView struct {
	Workspace string
	Conflicts []conflictRow
}

type conflictRow struct {
	Key            string
	File           string
	Field          string
	Base           string
	Main           string
	WorkspaceValue string
}

func StartWebServer(ctx context.Context, repo *Repository, addr string) error {
	tmpl, err := template.ParseFS(webAssets, "templates/*.html")
	if err != nil {
		return err
	}
	server := &webServer{repo: repo, templates: tmpl}
	mux := http.NewServeMux()
	server.routes(mux)

	httpServer := &http.Server{Addr: addr, Handler: mux}
	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		_ = httpServer.Shutdown(context.Background())
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *webServer) routes(mux *http.ServeMux) {
	staticFS, _ := fs.Sub(webAssets, "static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/workspaces", s.handleCreateWorkspace)
	mux.HandleFunc("/workspaces/save", s.handleSaveWorkspace)
	mux.HandleFunc("/workspaces/merge", s.handleMergeWorkspace)
	mux.HandleFunc("/workspaces/delete", s.handleDeleteWorkspace)
	mux.HandleFunc("/validate", s.handleValidate)
	mux.HandleFunc("/object", s.handleObject)
	mux.HandleFunc("/object/new", s.handleObjectNew)
	mux.HandleFunc("/object/save", s.handleObjectSave)
	mux.HandleFunc("/object/delete", s.handleObjectDelete)
}

func (s *webServer) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ws := workspaceFromRequest(r)
	data, err := s.loadDashboardData(ws)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	data.Flash = r.URL.Query().Get("flash")
	data.FlashError = r.URL.Query().Get("error") == "1"
	if err := s.templates.ExecuteTemplate(w, "dashboard.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *webServer) loadDashboardData(workspace string) (dashboardData, error) {
	repoPath, _, err := s.resolveWorkspacePath(workspace)
	if err != nil {
		return dashboardData{}, err
	}

	if workspace == "" {
		workspace = "main"
	}
	schemas, err := LoadSchemas(s.repo.Root)
	if err != nil {
		return dashboardData{}, err
	}
	typeNames := make([]string, 0, len(schemas))
	for t := range schemas {
		typeNames = append(typeNames, t)
	}
	sort.Strings(typeNames)

	types := make([]typeData, 0, len(typeNames))
	for _, t := range typeNames {
		objs, err := ListObjectsForType(repoPath, t)
		if err != nil {
			return dashboardData{}, err
		}
		types = append(types, typeData{Name: t, Objects: objs})
	}

	workspaces, err := s.repo.ListWorkspaces()
	if err != nil {
		return dashboardData{}, err
	}
	dirty := []string(nil)
	if workspace != "main" {
		for _, ws := range workspaces {
			if ws.Name == workspace {
				dirty = ws.ChangedFiles
				break
			}
		}
	}

	return dashboardData{
		RepoRoot:            s.repo.Root,
		CurrentWorkspace:    workspace,
		Workspaces:          workspaces,
		Types:               types,
		WorkspaceDirtyFiles: dirty,
	}, nil
}

func (s *webServer) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		s.redirectWithFlash(w, r, "/", "workspace name is required", true)
		return
	}
	if err := s.repo.CreateWorkspace(name); err != nil {
		s.redirectWithFlash(w, r, "/", err.Error(), true)
		return
	}
	s.redirectWithFlash(w, r, "/?ws="+url.QueryEscape(name), "workspace created", false)
}

func (s *webServer) handleSaveWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ws := strings.TrimSpace(r.FormValue("ws"))
	if ws == "" || ws == "main" {
		s.redirectWithFlash(w, r, "/", "select a workspace first", true)
		return
	}
	_, err := s.repo.SaveWorkspace(ws, strings.TrimSpace(r.FormValue("message")))
	if err != nil {
		s.redirectWithFlash(w, r, "/?ws="+url.QueryEscape(ws), err.Error(), true)
		return
	}
	s.redirectWithFlash(w, r, "/?ws="+url.QueryEscape(ws), "workspace committed", false)
}

func (s *webServer) handleMergeWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ws := strings.TrimSpace(r.FormValue("ws"))
	if ws == "" || ws == "main" {
		s.redirectWithFlash(w, r, "/", "select a workspace first", true)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resolutions := map[string]string{}
	manual := map[string]string{}
	for k, vals := range r.Form {
		if len(vals) == 0 {
			continue
		}
		if strings.HasPrefix(k, "resolve.") {
			resolutions[strings.TrimPrefix(k, "resolve.")] = vals[0]
		}
		if strings.HasPrefix(k, "manual.") {
			manual[strings.TrimPrefix(k, "manual.")] = vals[0]
		}
	}

	result, err := s.repo.MergeWorkspace(ws, resolutions, manual)
	if err != nil {
		s.redirectWithFlash(w, r, "/?ws="+url.QueryEscape(ws), err.Error(), true)
		return
	}
	if len(result.Conflicts) > 0 {
		rows := make([]conflictRow, 0, len(result.Conflicts))
		for _, c := range result.Conflicts {
			rows = append(rows, conflictRow{
				Key:            c.Key,
				File:           c.File,
				Field:          c.Field,
				Base:           valueToText(c.Base),
				Main:           valueToText(c.Main),
				WorkspaceValue: valueToText(c.Workspace),
			})
		}
		view := conflictView{Workspace: ws, Conflicts: rows}
		if err := s.templates.ExecuteTemplate(w, "merge_conflicts.html", view); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	s.redirectWithFlash(w, r, "/?ws=main", "workspace merged into main and deleted", false)
}

func (s *webServer) handleDeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ws := strings.TrimSpace(r.FormValue("ws"))
	if ws == "" || ws == "main" {
		s.redirectWithFlash(w, r, "/", "cannot delete main", true)
		return
	}
	if err := s.repo.DeleteWorkspace(ws); err != nil {
		s.redirectWithFlash(w, r, "/?ws="+url.QueryEscape(ws), err.Error(), true)
		return
	}
	s.redirectWithFlash(w, r, "/?ws=main", "workspace deleted", false)
}

func (s *webServer) handleValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ws := workspaceFromRequest(r)
	repoPath, _, err := s.resolveWorkspacePath(ws)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	data, err := s.loadDashboardData(ws)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := ValidateRepository(repoPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.ValidationRan = true
	data.ValidationOK = result.OK()
	data.ValidationIssues = result.Issues
	if err := s.templates.ExecuteTemplate(w, "dashboard.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *webServer) handleObject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.renderObjectPage(w, r, false)
}

func (s *webServer) handleObjectNew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.renderObjectPage(w, r, true)
}

func (s *webServer) renderObjectPage(w http.ResponseWriter, r *http.Request, isNew bool) {
	ws := workspaceFromRequest(r)
	typeName := strings.TrimSpace(r.URL.Query().Get("type"))
	if typeName == "" {
		http.Error(w, "type query parameter is required", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if isNew {
		id = ""
	}
	repoPath, readOnly, err := s.resolveWorkspacePath(ws)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	schemas, err := LoadSchemas(s.repo.Root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	schema, ok := schemas[typeName]
	if !ok {
		http.Error(w, "type schema not found", http.StatusNotFound)
		return
	}

	fields := schemaToFieldData(schema)
	values := map[string]string{}
	if id != "" {
		obj, err := ReadObject(repoPath, typeName, id)
		if err == nil {
			for k, v := range obj.Data {
				if k == "_id" || k == "_type" {
					continue
				}
				values[k] = valueToForm(v)
			}
		}
	}

	diffs := []fieldDiff{}
	if ws != "main" && id != "" {
		if mainObj, err := ReadObject(s.repo.Root, typeName, id); err == nil {
			if wsObj, err := ReadObject(repoPath, typeName, id); err == nil {
				diffs = computeDiffs(mainObj.Data, wsObj.Data)
			}
		}
	}

	data := objectData{
		Workspace:   ws,
		Type:        typeName,
		ID:          id,
		ReadOnly:    readOnly,
		Fields:      fields,
		FieldValues: values,
		Diffs:       diffs,
		Flash:       r.URL.Query().Get("flash"),
		FlashError:  r.URL.Query().Get("error") == "1",
	}
	if err := s.templates.ExecuteTemplate(w, "object.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *webServer) handleObjectSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ws := strings.TrimSpace(r.FormValue("ws"))
	typeName := strings.TrimSpace(r.FormValue("type"))
	id := strings.TrimSpace(r.FormValue("id"))
	if typeName == "" {
		http.Error(w, "type is required", http.StatusBadRequest)
		return
	}
	repoPath, readOnly, err := s.resolveWorkspacePath(ws)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if readOnly {
		s.redirectWithFlash(w, r, "/object?ws=main&type="+url.QueryEscape(typeName)+"&id="+url.QueryEscape(id), "main is read-only", true)
		return
	}
	if id == "" {
		id, err = NewUUID()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	schemas, err := LoadSchemas(s.repo.Root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	schema, ok := schemas[typeName]
	if !ok {
		http.Error(w, "type schema not found", http.StatusNotFound)
		return
	}

	obj := Object{ID: id, Type: typeName, Data: map[string]any{"_id": id, "_type": typeName}}
	for field, prop := range schema.Properties {
		raw := strings.TrimSpace(r.FormValue("field." + field))
		if raw == "" {
			continue
		}
		v, err := parseFormField(raw, prop)
		if err != nil {
			s.redirectWithFlash(w, r, "/object?ws="+url.QueryEscape(ws)+"&type="+url.QueryEscape(typeName)+"&id="+url.QueryEscape(id), fmt.Sprintf("invalid %s: %v", field, err), true)
			return
		}
		obj.Data[field] = v
	}

	if err := WriteObject(repoPath, obj); err != nil {
		s.redirectWithFlash(w, r, "/object?ws="+url.QueryEscape(ws)+"&type="+url.QueryEscape(typeName)+"&id="+url.QueryEscape(id), err.Error(), true)
		return
	}
	s.redirectWithFlash(w, r, "/object?ws="+url.QueryEscape(ws)+"&type="+url.QueryEscape(typeName)+"&id="+url.QueryEscape(id), "object updated", false)
}

func (s *webServer) handleObjectDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ws := strings.TrimSpace(r.FormValue("ws"))
	typeName := strings.TrimSpace(r.FormValue("type"))
	id := strings.TrimSpace(r.FormValue("id"))
	if typeName == "" || id == "" {
		http.Error(w, "type and id are required", http.StatusBadRequest)
		return
	}
	repoPath, readOnly, err := s.resolveWorkspacePath(ws)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if readOnly {
		s.redirectWithFlash(w, r, "/object?ws=main&type="+url.QueryEscape(typeName)+"&id="+url.QueryEscape(id), "main is read-only", true)
		return
	}
	if err := DeleteObject(repoPath, typeName, id); err != nil {
		s.redirectWithFlash(w, r, "/object?ws="+url.QueryEscape(ws)+"&type="+url.QueryEscape(typeName)+"&id="+url.QueryEscape(id), err.Error(), true)
		return
	}
	s.redirectWithFlash(w, r, "/?ws="+url.QueryEscape(ws), "object deleted", false)
}

func (s *webServer) resolveWorkspacePath(workspace string) (string, bool, error) {
	if workspace == "" || workspace == "main" {
		return s.repo.Root, true, nil
	}
	path := s.repo.WorkspacePath(workspace)
	if _, err := os.Stat(path); err != nil {
		return "", false, fmt.Errorf("workspace %q does not exist", workspace)
	}
	return path, false, nil
}

func workspaceFromRequest(r *http.Request) string {
	ws := strings.TrimSpace(r.FormValue("ws"))
	if ws == "" {
		ws = strings.TrimSpace(r.URL.Query().Get("ws"))
	}
	if ws == "" {
		return "main"
	}
	return ws
}

func (s *webServer) redirectWithFlash(w http.ResponseWriter, r *http.Request, rawURL, message string, isError bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		u = &url.URL{Path: "/"}
	}
	q := u.Query()
	q.Set("flash", message)
	if isError {
		q.Set("error", "1")
	} else {
		q.Del("error")
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

func schemaToFieldData(schema Schema) []fieldData {
	fields := make([]fieldData, 0, len(schema.Properties))
	for name, prop := range schema.Properties {
		_, required := schema.Required[name]
		fields = append(fields, fieldData{Name: name, Type: prop.Type, ItemsType: prop.ItemsType, Required: required, Enum: prop.Enum})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	return fields
}

func parseFormField(raw string, prop SchemaProperty) (any, error) {
	switch prop.Type {
	case "string":
		return raw, nil
	case "number", "integer":
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, err
		}
		if prop.Type == "integer" && n != float64(int64(n)) {
			return nil, errors.New("must be an integer")
		}
		return n, nil
	case "boolean":
		if raw == "true" {
			return true, nil
		}
		if raw == "false" {
			return false, nil
		}
		return nil, errors.New("must be true or false")
	case "array":
		if raw == "" {
			return []any{}, nil
		}
		parts := strings.Split(raw, ",")
		arr := make([]any, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if prop.ItemsType == "string" {
				arr = append(arr, p)
			} else {
				n, err := strconv.ParseFloat(p, 64)
				if err != nil {
					return nil, err
				}
				if prop.ItemsType == "integer" && n != float64(int64(n)) {
					return nil, errors.New("array items must be integers")
				}
				arr = append(arr, n)
			}
		}
		return arr, nil
	default:
		return nil, fmt.Errorf("unsupported type %s", prop.Type)
	}
}

func computeDiffs(mainData, wsData map[string]any) []fieldDiff {
	keys := map[string]struct{}{}
	for k := range mainData {
		if k == "_id" || k == "_type" {
			continue
		}
		keys[k] = struct{}{}
	}
	for k := range wsData {
		if k == "_id" || k == "_type" {
			continue
		}
		keys[k] = struct{}{}
	}
	fields := make([]string, 0, len(keys))
	for k := range keys {
		fields = append(fields, k)
	}
	sort.Strings(fields)

	diffs := make([]fieldDiff, 0)
	for _, field := range fields {
		m, mOK := mainData[field]
		w, wOK := wsData[field]
		status := "unchanged"
		switch {
		case !mOK && wOK:
			status = "added"
		case mOK && !wOK:
			status = "removed"
		case mOK && wOK && valueToText(m) != valueToText(w):
			status = "modified"
		default:
			continue
		}
		diffs = append(diffs, fieldDiff{Field: field, Main: valueToText(m), Workspace: valueToText(w), Status: status})
	}
	return diffs
}

func valueToText(v any) string {
	switch t := v.(type) {
	case nil:
		return "<empty>"
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return formatNumber(t)
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, valueToText(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func valueToForm(v any) string {
	switch t := v.(type) {
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, valueToText(item))
		}
		return strings.Join(parts, ",")
	default:
		return valueToText(v)
	}
}
