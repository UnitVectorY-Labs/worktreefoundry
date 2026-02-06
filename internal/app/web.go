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
	"time"
)

type webServer struct {
	repo      *Repository
	templates *template.Template
}

type workspaceOption struct {
	Name  string
	Dirty bool
}

type topBarData struct {
	RepoName       string
	Workspace      string
	WorkspaceDirty bool
	OnMain         bool
	Workspaces     []workspaceOption
	CurrentPath    string
}

type pageBase struct {
	Top        topBarData
	Flash      string
	FlashError bool
}

type typesPageData struct {
	pageBase
	Types []typeSummary
}

type typeSummary struct {
	Name       string
	Count      int
	DirtyCount int
}

type typePageData struct {
	pageBase
	TypeName      string
	ReadOnly      bool
	DisplayField  string
	ExtraFields   []string
	Items         []objectListItem
	TypeConfigURL string
	NewItemURL    string
}

type objectListItem struct {
	ID         string
	Display    string
	Fields     []namedValue
	Dirty      string
	Deleted    bool
	OpenURL    string
	DeleteURL  string
	RestoreURL string
}

type namedValue struct {
	Name  string
	Value string
}

type objectPageData struct {
	pageBase
	TypeName      string
	ID            string
	ReadOnly      bool
	Missing       bool
	MissingReason string
	CanRestore    bool
	RestoreURL    string
	WriteURL      string
	DeleteURL     string
	BackURL       string
	Fields        []fieldData
	FieldValues   map[string]string
	Diffs         []fieldDiff
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

type workspaceNewPageData struct {
	pageBase
	CreateURL string
}

type configPageData struct {
	pageBase
	ReadOnly     bool
	RepoName     string
	SaveURL      string
	TypeSettings []typeSettingLink
}

type typeSettingLink struct {
	TypeName string
	URL      string
}

type typeConfigPageData struct {
	pageBase
	ReadOnly        bool
	TypeName        string
	DisplayOptions  []displayOption
	ExtraOptions    []extraOption
	SaveURL         string
	BackURL         string
	CurrentRepoName string
}

type displayOption struct {
	Name     string
	Selected bool
}

type extraOption struct {
	Name    string
	Checked bool
}

type conflictView struct {
	pageBase
	Workspace string
	Conflicts []conflictRow
	PostURL   string
	BackURL   string
}

type conflictRow struct {
	Key            string
	File           string
	Field          string
	Base           string
	Main           string
	WorkspaceValue string
}

type workspaceContext struct {
	Workspace      string
	RepoPath       string
	ReadOnly       bool
	Schemas        map[string]Schema
	UI             UIConfig
	Workspaces     []Workspace
	WorkspaceDirty bool
	DirtyByType    map[string]map[string]string
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
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/w/", s.handleWorkspace)
}

func (s *webServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/w/main/types", http.StatusSeeOther)
}

func (s *webServer) handleWorkspace(w http.ResponseWriter, r *http.Request) {
	ws, tail, ok := parseWorkspacePath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch {
	case len(tail) == 0:
		http.Redirect(w, r, "/w/"+url.PathEscape(ws)+"/types", http.StatusSeeOther)
		return
	case len(tail) == 1 && tail[0] == "types" && r.Method == http.MethodGet:
		s.handleTypesHome(w, r, ws)
		return
	case len(tail) == 2 && tail[0] == "types" && r.Method == http.MethodGet:
		s.handleTypeList(w, r, ws, tail[1])
		return
	case len(tail) == 3 && tail[0] == "types" && tail[2] == "new" && r.Method == http.MethodGet:
		s.handleObjectPage(w, r, ws, tail[1], "")
		return
	case len(tail) == 4 && tail[0] == "types" && tail[2] == "objects" && r.Method == http.MethodGet:
		s.handleObjectPage(w, r, ws, tail[1], tail[3])
		return
	case len(tail) == 4 && tail[0] == "types" && tail[2] == "objects" && tail[3] == "write" && r.Method == http.MethodPost:
		s.handleObjectWrite(w, r, ws, tail[1])
		return
	case len(tail) == 5 && tail[0] == "types" && tail[2] == "objects" && tail[4] == "delete" && r.Method == http.MethodPost:
		s.handleObjectDelete(w, r, ws, tail[1], tail[3])
		return
	case len(tail) == 5 && tail[0] == "types" && tail[2] == "objects" && tail[4] == "restore" && r.Method == http.MethodPost:
		s.handleObjectRestore(w, r, ws, tail[1], tail[3])
		return
	case len(tail) == 2 && tail[0] == "workspace" && tail[1] == "new" && r.Method == http.MethodGet:
		s.handleWorkspaceNewPage(w, r, ws)
		return
	case len(tail) == 2 && tail[0] == "workspace" && tail[1] == "new" && r.Method == http.MethodPost:
		s.handleWorkspaceCreate(w, r, ws)
		return
	case len(tail) == 2 && tail[0] == "workspace" && tail[1] == "delete" && r.Method == http.MethodPost:
		s.handleWorkspaceDelete(w, r, ws)
		return
	case len(tail) == 1 && tail[0] == "save" && r.Method == http.MethodPost:
		s.handleWorkspaceSave(w, r, ws)
		return
	case len(tail) == 1 && tail[0] == "promote" && r.Method == http.MethodPost:
		s.handleWorkspacePromote(w, r, ws)
		return
	case len(tail) == 1 && tail[0] == "validate" && r.Method == http.MethodPost:
		s.handleWorkspaceValidate(w, r, ws)
		return
	case len(tail) == 1 && tail[0] == "config" && r.Method == http.MethodGet:
		s.handleConfigPage(w, r, ws)
		return
	case len(tail) == 1 && tail[0] == "config" && r.Method == http.MethodPost:
		s.handleConfigSave(w, r, ws)
		return
	case len(tail) == 3 && tail[0] == "config" && tail[1] == "types" && r.Method == http.MethodGet:
		s.handleTypeConfigPage(w, r, ws, tail[2])
		return
	case len(tail) == 3 && tail[0] == "config" && tail[1] == "types" && r.Method == http.MethodPost:
		s.handleTypeConfigSave(w, r, ws, tail[2])
		return
	default:
		http.NotFound(w, r)
		return
	}
}

func parseWorkspacePath(path string) (workspace string, tail []string, ok bool) {
	parts := splitPath(path)
	if len(parts) < 2 || parts[0] != "w" {
		return "", nil, false
	}
	if parts[1] == "" {
		return "", nil, false
	}
	return parts[1], parts[2:], true
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func (s *webServer) loadContext(workspace string) (workspaceContext, error) {
	repoPath, readOnly, err := s.resolveWorkspacePath(workspace)
	if err != nil {
		return workspaceContext{}, err
	}
	schemas, err := LoadSchemas(repoPath)
	if err != nil {
		return workspaceContext{}, err
	}
	ui, err := LoadUIConfig(repoPath, schemas)
	if err != nil {
		return workspaceContext{}, err
	}
	workspaces, err := s.repo.ListWorkspaces()
	if err != nil {
		return workspaceContext{}, err
	}
	ctx := workspaceContext{
		Workspace:   workspace,
		RepoPath:    repoPath,
		ReadOnly:    readOnly,
		Schemas:     schemas,
		UI:          ui,
		Workspaces:  workspaces,
		DirtyByType: map[string]map[string]string{},
	}
	if !readOnly {
		entries, err := s.repo.ChangedEntries(repoPath)
		if err != nil {
			return workspaceContext{}, err
		}
		ctx.DirtyByType = mapDirtyEntries(entries)
	}
	for _, ws := range workspaces {
		if ws.Name == workspace {
			ctx.WorkspaceDirty = ws.Dirty
			break
		}
	}
	return ctx, nil
}

func (s *webServer) topBar(ctx workspaceContext, currentPath string) topBarData {
	options := []workspaceOption{{Name: "main", Dirty: false}}
	for _, ws := range ctx.Workspaces {
		options = append(options, workspaceOption{Name: ws.Name, Dirty: ws.Dirty})
	}
	return topBarData{
		RepoName:       ctx.UI.RepoName,
		Workspace:      ctx.Workspace,
		WorkspaceDirty: ctx.WorkspaceDirty,
		OnMain:         ctx.ReadOnly,
		Workspaces:     options,
		CurrentPath:    currentPath,
	}
}

func (s *webServer) handleTypesHome(w http.ResponseWriter, r *http.Request, workspace string) {
	ctx, err := s.loadContext(workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	types := make([]string, 0, len(ctx.Schemas))
	for t := range ctx.Schemas {
		types = append(types, t)
	}
	sort.Strings(types)

	summaries := make([]typeSummary, 0, len(types))
	for _, t := range types {
		objs, err := ListObjectsForType(ctx.RepoPath, t)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		dirtyCount := len(ctx.DirtyByType[t])
		summaries = append(summaries, typeSummary{Name: t, Count: len(objs), DirtyCount: dirtyCount})
	}

	data := typesPageData{
		pageBase: pageBase{
			Top:        s.topBar(ctx, r.URL.Path),
			Flash:      r.URL.Query().Get("flash"),
			FlashError: r.URL.Query().Get("error") == "1",
		},
		Types: summaries,
	}
	s.renderTemplate(w, "types.html", data)
}

func (s *webServer) handleTypeList(w http.ResponseWriter, r *http.Request, workspace, typeName string) {
	ctx, err := s.loadContext(workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	schema, ok := ctx.Schemas[typeName]
	if !ok {
		http.NotFound(w, r)
		return
	}
	objects, err := ListObjectsForType(ctx.RepoPath, typeName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	typeCfg := ctx.UI.Types[typeName]
	if typeCfg.DisplayField == "" {
		typeCfg.DisplayField = "_id"
	}
	extraFields := selectedExtraFields(typeCfg.Fields, schema, typeCfg.DisplayField)

	items := make([]objectListItem, 0, len(objects))
	seen := map[string]struct{}{}
	for _, obj := range objects {
		seen[obj.ID] = struct{}{}
		dirty := ctx.DirtyByType[typeName][obj.ID]
		fields := make([]namedValue, 0, len(extraFields))
		for _, field := range extraFields {
			fields = append(fields, namedValue{Name: field, Value: valueToText(obj.Data[field])})
		}
		idPath := url.PathEscape(obj.ID)
		typePath := url.PathEscape(typeName)
		items = append(items, objectListItem{
			ID:        obj.ID,
			Display:   displayValue(obj.Data, typeCfg.DisplayField, obj.ID),
			Fields:    fields,
			Dirty:     dirty,
			Deleted:   false,
			OpenURL:   "/w/" + url.PathEscape(workspace) + "/types/" + typePath + "/objects/" + idPath,
			DeleteURL: "/w/" + url.PathEscape(workspace) + "/types/" + typePath + "/objects/" + idPath + "/delete",
		})
	}

	for id, status := range ctx.DirtyByType[typeName] {
		if status != "D" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		deletedDisplay := id
		deletedFields := make([]namedValue, 0, len(extraFields))
		if baseObj, err := ReadObject(s.repo.Root, typeName, id); err == nil {
			deletedDisplay = displayValue(baseObj.Data, typeCfg.DisplayField, id)
			for _, field := range extraFields {
				deletedFields = append(deletedFields, namedValue{Name: field, Value: valueToText(baseObj.Data[field])})
			}
		}
		typePath := url.PathEscape(typeName)
		idPath := url.PathEscape(id)
		items = append(items, objectListItem{
			ID:         id,
			Display:    deletedDisplay,
			Fields:     deletedFields,
			Dirty:      status,
			Deleted:    true,
			RestoreURL: "/w/" + url.PathEscape(workspace) + "/types/" + typePath + "/objects/" + idPath + "/restore",
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Deleted != items[j].Deleted {
			return !items[i].Deleted
		}
		if items[i].Display == items[j].Display {
			return items[i].ID < items[j].ID
		}
		return items[i].Display < items[j].Display
	})

	data := typePageData{
		pageBase: pageBase{
			Top:        s.topBar(ctx, r.URL.Path),
			Flash:      r.URL.Query().Get("flash"),
			FlashError: r.URL.Query().Get("error") == "1",
		},
		TypeName:      typeName,
		ReadOnly:      ctx.ReadOnly,
		DisplayField:  typeCfg.DisplayField,
		ExtraFields:   extraFields,
		Items:         items,
		TypeConfigURL: "/w/" + url.PathEscape(workspace) + "/config/types/" + url.PathEscape(typeName),
		NewItemURL:    "/w/" + url.PathEscape(workspace) + "/types/" + url.PathEscape(typeName) + "/new",
	}
	s.renderTemplate(w, "type.html", data)
}

func (s *webServer) handleObjectPage(w http.ResponseWriter, r *http.Request, workspace, typeName, id string) {
	ctx, err := s.loadContext(workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	schema, ok := ctx.Schemas[typeName]
	if !ok {
		http.NotFound(w, r)
		return
	}

	data := objectPageData{
		pageBase: pageBase{
			Top:        s.topBar(ctx, r.URL.Path),
			Flash:      r.URL.Query().Get("flash"),
			FlashError: r.URL.Query().Get("error") == "1",
		},
		TypeName:    typeName,
		ID:          id,
		ReadOnly:    ctx.ReadOnly,
		Fields:      schemaToFieldData(schema),
		FieldValues: map[string]string{},
		BackURL:     "/w/" + url.PathEscape(workspace) + "/types/" + url.PathEscape(typeName),
		WriteURL:    "/w/" + url.PathEscape(workspace) + "/types/" + url.PathEscape(typeName) + "/objects/write",
	}
	if id != "" {
		data.DeleteURL = "/w/" + url.PathEscape(workspace) + "/types/" + url.PathEscape(typeName) + "/objects/" + url.PathEscape(id) + "/delete"
		data.RestoreURL = "/w/" + url.PathEscape(workspace) + "/types/" + url.PathEscape(typeName) + "/objects/" + url.PathEscape(id) + "/restore"
	}

	if id == "" {
		s.renderTemplate(w, "object.html", data)
		return
	}

	obj, err := ReadObject(ctx.RepoPath, typeName, id)
	if err != nil {
		data.Missing = true
		data.MissingReason = "Object was not found in this workspace."
		if !ctx.ReadOnly && ctx.DirtyByType[typeName][id] == "D" {
			data.CanRestore = true
			data.MissingReason = "Object is currently marked deleted in this workspace."
		}
		s.renderTemplate(w, "object.html", data)
		return
	}
	for k, v := range obj.Data {
		if k == "_id" || k == "_type" {
			continue
		}
		data.FieldValues[k] = valueToForm(v)
	}
	if workspace != "main" {
		if mainObj, err := ReadObject(s.repo.Root, typeName, id); err == nil {
			data.Diffs = computeDiffs(mainObj.Data, obj.Data)
		}
	}
	s.renderTemplate(w, "object.html", data)
}

func (s *webServer) handleObjectWrite(w http.ResponseWriter, r *http.Request, workspace, typeName string) {
	ctx, err := s.loadContext(workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if ctx.ReadOnly {
		s.redirectWithFlash(w, r, "/w/main/types/"+url.PathEscape(typeName), "main is read-only", true)
		return
	}
	schema, ok := ctx.Schemas[typeName]
	if !ok {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	if id == "" {
		id, err = NewUUID()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	obj := Object{ID: id, Type: typeName, Data: map[string]any{"_id": id, "_type": typeName}}
	for field, prop := range schema.Properties {
		raw := strings.TrimSpace(r.FormValue("field." + field))
		if raw == "" {
			continue
		}
		v, err := parseFormField(raw, prop)
		if err != nil {
			path := "/w/" + url.PathEscape(workspace) + "/types/" + url.PathEscape(typeName) + "/objects/" + url.PathEscape(id)
			s.redirectWithFlash(w, r, path, fmt.Sprintf("invalid %s: %v", field, err), true)
			return
		}
		obj.Data[field] = v
	}

	if err := WriteObject(ctx.RepoPath, obj); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	path := "/w/" + url.PathEscape(workspace) + "/types/" + url.PathEscape(typeName) + "/objects/" + url.PathEscape(id)
	s.redirectWithFlash(w, r, path, "Draft updated", false)
}

func (s *webServer) handleObjectDelete(w http.ResponseWriter, r *http.Request, workspace, typeName, id string) {
	ctx, err := s.loadContext(workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if ctx.ReadOnly {
		s.redirectWithFlash(w, r, "/w/main/types/"+url.PathEscape(typeName), "main is read-only", true)
		return
	}
	if err := DeleteObject(ctx.RepoPath, typeName, id); err != nil {
		s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/types/"+url.PathEscape(typeName), err.Error(), true)
		return
	}
	s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/types/"+url.PathEscape(typeName), "Object deleted in draft", false)
}

func (s *webServer) handleObjectRestore(w http.ResponseWriter, r *http.Request, workspace, typeName, id string) {
	if workspace == "main" {
		s.redirectWithFlash(w, r, "/w/main/types/"+url.PathEscape(typeName), "main is read-only", true)
		return
	}
	if err := s.repo.RestoreObject(workspace, typeName, id); err != nil {
		s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/types/"+url.PathEscape(typeName), err.Error(), true)
		return
	}
	s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/types/"+url.PathEscape(typeName), "Object restored", false)
}

func (s *webServer) handleWorkspaceNewPage(w http.ResponseWriter, r *http.Request, workspace string) {
	ctx, err := s.loadContext(workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	data := workspaceNewPageData{
		pageBase: pageBase{
			Top:        s.topBar(ctx, r.URL.Path),
			Flash:      r.URL.Query().Get("flash"),
			FlashError: r.URL.Query().Get("error") == "1",
		},
		CreateURL: "/w/" + url.PathEscape(workspace) + "/workspace/new",
	}
	s.renderTemplate(w, "workspace_new.html", data)
}

func (s *webServer) handleWorkspaceCreate(w http.ResponseWriter, r *http.Request, workspace string) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/workspace/new", "workspace name is required", true)
		return
	}
	if err := s.repo.CreateWorkspace(name); err != nil {
		s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/workspace/new", err.Error(), true)
		return
	}
	s.redirectWithFlash(w, r, "/w/"+url.PathEscape(name)+"/types", "Workspace created", false)
}

func (s *webServer) handleWorkspaceDelete(w http.ResponseWriter, r *http.Request, workspace string) {
	if workspace == "main" {
		s.redirectWithFlash(w, r, "/w/main/types", "main cannot be deleted", true)
		return
	}
	if err := s.repo.DeleteWorkspace(workspace); err != nil {
		s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/types", err.Error(), true)
		return
	}
	s.redirectWithFlash(w, r, "/w/main/types", "Workspace deleted", false)
}

func (s *webServer) handleWorkspaceSave(w http.ResponseWriter, r *http.Request, workspace string) {
	if workspace == "main" {
		s.redirectWithFlash(w, r, "/w/main/types", "main is read-only", true)
		return
	}
	returnPath := firstNonEmpty(r.FormValue("return"), "/w/"+url.PathEscape(workspace)+"/types")
	msg := "Save workspace " + workspace + " at " + time.Now().Format("2006-01-02 15:04:05")
	if _, err := s.repo.SaveWorkspace(workspace, msg); err != nil {
		s.redirectWithFlash(w, r, returnPath, err.Error(), true)
		return
	}
	s.redirectWithFlash(w, r, returnPath, "Workspace saved", false)
}

func (s *webServer) handleWorkspacePromote(w http.ResponseWriter, r *http.Request, workspace string) {
	if workspace == "main" {
		s.redirectWithFlash(w, r, "/w/main/types", "main cannot be promoted", true)
		return
	}
	returnPath := firstNonEmpty(r.FormValue("return"), "/w/"+url.PathEscape(workspace)+"/types")
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resolutions := map[string]string{}
	manual := map[string]string{}
	for key, vals := range r.Form {
		if len(vals) == 0 {
			continue
		}
		if strings.HasPrefix(key, "resolve.") {
			resolutions[strings.TrimPrefix(key, "resolve.")] = vals[0]
		}
		if strings.HasPrefix(key, "manual.") {
			manual[strings.TrimPrefix(key, "manual.")] = vals[0]
		}
	}
	result, err := s.repo.MergeWorkspace(workspace, resolutions, manual)
	if err != nil {
		s.redirectWithFlash(w, r, returnPath, err.Error(), true)
		return
	}
	if len(result.Conflicts) > 0 {
		ctx, err := s.loadContext(workspace)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
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
		data := conflictView{
			pageBase:  pageBase{Top: s.topBar(ctx, r.URL.Path)},
			Workspace: workspace,
			Conflicts: rows,
			PostURL:   "/w/" + url.PathEscape(workspace) + "/promote",
			BackURL:   returnPath,
		}
		s.renderTemplate(w, "promote_conflicts.html", data)
		return
	}
	s.redirectWithFlash(w, r, "/w/main/types", "Workspace promoted to main", false)
}

func (s *webServer) handleWorkspaceValidate(w http.ResponseWriter, r *http.Request, workspace string) {
	ctx, err := s.loadContext(workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	returnPath := firstNonEmpty(r.FormValue("return"), "/w/"+url.PathEscape(workspace)+"/types")
	result, err := ValidateRepository(ctx.RepoPath)
	if err != nil {
		s.redirectWithFlash(w, r, returnPath, err.Error(), true)
		return
	}
	if !result.OK() {
		s.redirectWithFlash(w, r, returnPath, result.Issues[0].String(), true)
		return
	}
	s.redirectWithFlash(w, r, returnPath, "Validation passed", false)
}

func (s *webServer) handleConfigPage(w http.ResponseWriter, r *http.Request, workspace string) {
	ctx, err := s.loadContext(workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	types := make([]string, 0, len(ctx.Schemas))
	for t := range ctx.Schemas {
		types = append(types, t)
	}
	sort.Strings(types)
	links := make([]typeSettingLink, 0, len(types))
	for _, t := range types {
		links = append(links, typeSettingLink{TypeName: t, URL: "/w/" + url.PathEscape(workspace) + "/config/types/" + url.PathEscape(t)})
	}
	data := configPageData{
		pageBase: pageBase{
			Top:        s.topBar(ctx, r.URL.Path),
			Flash:      r.URL.Query().Get("flash"),
			FlashError: r.URL.Query().Get("error") == "1",
		},
		ReadOnly:     ctx.ReadOnly,
		RepoName:     ctx.UI.RepoName,
		SaveURL:      "/w/" + url.PathEscape(workspace) + "/config",
		TypeSettings: links,
	}
	s.renderTemplate(w, "config.html", data)
}

func (s *webServer) handleConfigSave(w http.ResponseWriter, r *http.Request, workspace string) {
	ctx, err := s.loadContext(workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if ctx.ReadOnly {
		s.redirectWithFlash(w, r, "/w/main/config", "main is read-only", true)
		return
	}
	cfg := ctx.UI
	cfg.RepoName = strings.TrimSpace(r.FormValue("repoName"))
	for _, issue := range ValidateUIConfig(cfg, ctx.Schemas) {
		s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/config", issue.String(), true)
		return
	}
	if err := SaveUIConfig(ctx.RepoPath, cfg); err != nil {
		s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/config", err.Error(), true)
		return
	}
	s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/config", "Configuration draft updated", false)
}

func (s *webServer) handleTypeConfigPage(w http.ResponseWriter, r *http.Request, workspace, typeName string) {
	ctx, err := s.loadContext(workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	schema, ok := ctx.Schemas[typeName]
	if !ok {
		http.NotFound(w, r)
		return
	}
	tc := ctx.UI.Types[typeName]
	if tc.DisplayField == "" {
		tc.DisplayField = "_id"
	}
	displayOptions := []displayOption{{Name: "_id", Selected: tc.DisplayField == "_id"}}
	required := make([]string, 0, len(schema.Required))
	for req := range schema.Required {
		required = append(required, req)
	}
	sort.Strings(required)
	for _, req := range required {
		displayOptions = append(displayOptions, displayOption{Name: req, Selected: tc.DisplayField == req})
	}

	extraOrder := orderedFieldOptions(tc.Fields, schema, tc.DisplayField)
	extraOptions := make([]extraOption, 0, len(extraOrder))
	for _, field := range extraOrder {
		extraOptions = append(extraOptions, extraOption{Name: field, Checked: contains(tc.Fields, field)})
	}

	data := typeConfigPageData{
		pageBase: pageBase{
			Top:        s.topBar(ctx, r.URL.Path),
			Flash:      r.URL.Query().Get("flash"),
			FlashError: r.URL.Query().Get("error") == "1",
		},
		ReadOnly:        ctx.ReadOnly,
		TypeName:        typeName,
		DisplayOptions:  displayOptions,
		ExtraOptions:    extraOptions,
		SaveURL:         "/w/" + url.PathEscape(workspace) + "/config/types/" + url.PathEscape(typeName),
		BackURL:         "/w/" + url.PathEscape(workspace) + "/types/" + url.PathEscape(typeName),
		CurrentRepoName: ctx.UI.RepoName,
	}
	s.renderTemplate(w, "type_config.html", data)
}

func (s *webServer) handleTypeConfigSave(w http.ResponseWriter, r *http.Request, workspace, typeName string) {
	ctx, err := s.loadContext(workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if ctx.ReadOnly {
		s.redirectWithFlash(w, r, "/w/main/config/types/"+url.PathEscape(typeName), "main is read-only", true)
		return
	}
	if _, ok := ctx.Schemas[typeName]; !ok {
		http.NotFound(w, r)
		return
	}
	cfg := ctx.UI
	if cfg.Types == nil {
		cfg.Types = map[string]TypeUIConfig{}
	}
	tc := cfg.Types[typeName]
	tc.DisplayField = strings.TrimSpace(r.FormValue("displayField"))
	if tc.DisplayField == "" {
		tc.DisplayField = "_id"
	}
	tc.Fields = dedupeOrdered(r.Form["extraField"])
	cfg.Types[typeName] = tc

	for _, issue := range ValidateUIConfig(cfg, ctx.Schemas) {
		s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/config/types/"+url.PathEscape(typeName), issue.String(), true)
		return
	}
	if err := SaveUIConfig(ctx.RepoPath, cfg); err != nil {
		s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/config/types/"+url.PathEscape(typeName), err.Error(), true)
		return
	}
	s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/config/types/"+url.PathEscape(typeName), "Type configuration draft updated", false)
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

func (s *webServer) renderTemplate(w http.ResponseWriter, name string, data any) {
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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

func mapDirtyEntries(entries []ChangedEntry) map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, entry := range entries {
		typeName, id, ok := parseDataObjectPath(entry.Path)
		if !ok {
			continue
		}
		if _, ok := out[typeName]; !ok {
			out[typeName] = map[string]string{}
		}
		out[typeName][id] = entry.Status
	}
	return out
}

func parseDataObjectPath(path string) (typeName, id string, ok bool) {
	if !strings.HasPrefix(path, "data/") || !strings.HasSuffix(path, ".yaml") {
		return "", "", false
	}
	parts := strings.Split(path, "/")
	if len(parts) != 3 {
		return "", "", false
	}
	return parts[1], strings.TrimSuffix(parts[2], ".yaml"), true
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
		return ""
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
		return strings.Join(parts, ", ")
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

func displayValue(data map[string]any, field, fallbackID string) string {
	if field == "" || field == "_id" {
		return fallbackID
	}
	if v, ok := data[field]; ok {
		text := valueToText(v)
		if text != "" {
			return text
		}
	}
	return fallbackID
}

func selectedExtraFields(configured []string, schema Schema, displayField string) []string {
	configured = dedupeOrdered(configured)
	out := make([]string, 0)
	for _, f := range configured {
		if f == displayField {
			continue
		}
		if _, ok := schema.Properties[f]; !ok {
			continue
		}
		out = append(out, f)
	}
	return out
}

func orderedFieldOptions(configured []string, schema Schema, displayField string) []string {
	selected := selectedExtraFields(configured, schema, displayField)
	seen := map[string]struct{}{}
	for _, f := range selected {
		seen[f] = struct{}{}
	}
	remaining := make([]string, 0)
	for field := range schema.Properties {
		if field == displayField {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		remaining = append(remaining, field)
	}
	sort.Strings(remaining)
	return append(selected, remaining...)
}

func contains(values []string, candidate string) bool {
	for _, v := range values {
		if v == candidate {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
