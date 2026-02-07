package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	Crumbs     []breadcrumb
	Flash      string
	FlashError bool
}

type breadcrumb struct {
	Label   string
	URL     string
	Current bool
}

type typesPageData struct {
	pageBase
	Types []typeSummary
}

type typeSummary struct {
	Name       string
	Count      int
	DirtyCount int
	ConfigURL  string
}

type typePageData struct {
	pageBase
	TypeName       string
	ReadOnly       bool
	DisplayField   string
	PrimaryHeading string
	ExtraFields    []string
	Items          []objectListItem
	TypeConfigURL  string
	NewItemURL     string
}

type objectListItem struct {
	ID            string
	Display       string
	PrimaryURL    string
	Fields        []namedValue
	Dirty         string
	Deleted       bool
	RestoreURL    string
	Invalid       bool
	InvalidCount  int
	InvalidSample string
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
	Fields        []fieldData
	FieldValues   map[string]string
	Diffs         []fieldDiff
	InvalidIssues []ValidationIssue
}

type fieldData struct {
	Name       string
	Type       string
	ItemsType  string
	Required   bool
	Unique     bool
	Enum       []string
	MinLength  string
	MaxLength  string
	Minimum    string
	Maximum    string
	ForeignKey *foreignKeyField
}

type foreignKeyField struct {
	ValueField   string
	DisplayField string
	Options      []foreignKeyOption
}

type foreignKeyOption struct {
	Value   string
	Display string
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
	ReadOnly       bool
	RepoName       string
	SaveURL        string
	TypeSettings   []typeSettingLink
	SchemaLinks    []typeSettingLink
	ConstraintsURL string
	NewSchemaURL   string
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
	Order   int
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

type confirmSavePageData struct {
	pageBase
	Workspace string
	Changes   []confirmChange
	PostURL   string
	BackURL   string
}

type confirmMergePageData struct {
	pageBase
	Workspace string
	Changes   []confirmChange
	PostURL   string
	BackURL   string
}

type confirmChange struct {
	File   string
	Status string
}

type schemaEditPageData struct {
	pageBase
	ReadOnly bool
	TypeName string
	Content  string
	SaveURL  string
	BackURL  string
}

type constraintsEditPageData struct {
	pageBase
	ReadOnly bool
	Content  string
	SaveURL  string
	BackURL  string
}

type workspaceContext struct {
	Workspace      string
	RepoPath       string
	ReadOnly       bool
	Schemas        map[string]Schema
	Constraints    Constraints
	UI             UIConfig
	Workspaces     []Workspace
	WorkspaceDirty bool
	DirtyByType    map[string]map[string]string
	ObjectIssues   map[string]map[string][]ValidationIssue
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
	case len(tail) == 2 && tail[0] == "save" && tail[1] == "confirm" && r.Method == http.MethodGet:
		s.handleSaveConfirmPage(w, r, ws)
		return
	case len(tail) == 1 && tail[0] == "save" && r.Method == http.MethodPost:
		s.handleWorkspaceSave(w, r, ws)
		return
	case len(tail) == 2 && tail[0] == "merge" && tail[1] == "confirm" && r.Method == http.MethodGet:
		s.handleMergeConfirmPage(w, r, ws)
		return
	case len(tail) == 1 && tail[0] == "merge" && r.Method == http.MethodPost:
		s.handleWorkspaceMerge(w, r, ws)
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
	case len(tail) == 4 && tail[0] == "config" && tail[1] == "schemas" && r.Method == http.MethodGet:
		s.handleSchemaEditPage(w, r, ws, tail[2], tail[3])
		return
	case len(tail) == 4 && tail[0] == "config" && tail[1] == "schemas" && r.Method == http.MethodPost:
		s.handleSchemaEditSave(w, r, ws, tail[2], tail[3])
		return
	case len(tail) == 2 && tail[0] == "config" && tail[1] == "constraints" && r.Method == http.MethodGet:
		s.handleConstraintsEditPage(w, r, ws)
		return
	case len(tail) == 2 && tail[0] == "config" && tail[1] == "constraints" && r.Method == http.MethodPost:
		s.handleConstraintsEditSave(w, r, ws)
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
	constraints, err := LoadConstraints(repoPath)
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
		Workspace:    workspace,
		RepoPath:     repoPath,
		ReadOnly:     readOnly,
		Schemas:      schemas,
		Constraints:  constraints,
		UI:           ui,
		Workspaces:   workspaces,
		DirtyByType:  map[string]map[string]string{},
		ObjectIssues: map[string]map[string][]ValidationIssue{},
	}
	objectIssues, err := collectObjectIssues(repoPath)
	if err != nil {
		return workspaceContext{}, err
	}
	ctx.ObjectIssues = objectIssues
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
		summaries = append(summaries, typeSummary{
			Name:       t,
			Count:      len(objs),
			DirtyCount: dirtyCount,
			ConfigURL:  "/w/" + url.PathEscape(workspace) + "/config/types/" + url.PathEscape(t),
		})
	}

	data := typesPageData{
		pageBase: pageBase{
			Top:        s.topBar(ctx, r.URL.Path),
			Crumbs:     buildCrumbs(workspace),
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
	primaryHeading := typeCfg.DisplayField
	if primaryHeading == "_id" || primaryHeading == "" {
		primaryHeading = "_id"
	}

	items := make([]objectListItem, 0, len(objects))
	seen := map[string]struct{}{}
	for _, obj := range objects {
		seen[obj.ID] = struct{}{}
		dirty := ctx.DirtyByType[typeName][obj.ID]
		fields := make([]namedValue, 0, len(extraFields))
		for _, field := range extraFields {
			fields = append(fields, namedValue{Name: field, Value: valueToText(obj.Data[field])})
		}
		issues := ctx.ObjectIssues[typeName][obj.ID]
		invalid := len(issues) > 0
		invalidSample := ""
		if invalid {
			invalidSample = issues[0].Message
		}
		idPath := url.PathEscape(obj.ID)
		typePath := url.PathEscape(typeName)
		primaryURL := "/w/" + url.PathEscape(workspace) + "/types/" + typePath + "/objects/" + idPath
		items = append(items, objectListItem{
			ID:            obj.ID,
			Display:       displayValue(obj.Data, typeCfg.DisplayField, obj.ID),
			PrimaryURL:    primaryURL,
			Fields:        fields,
			Dirty:         dirty,
			Deleted:       false,
			Invalid:       invalid,
			InvalidCount:  len(issues),
			InvalidSample: invalidSample,
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
		primaryURL := "/w/" + url.PathEscape(workspace) + "/types/" + typePath + "/objects/" + idPath
		items = append(items, objectListItem{
			ID:         id,
			Display:    deletedDisplay,
			PrimaryURL: primaryURL,
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
			Crumbs:     buildCrumbs(workspace, typeName),
			Flash:      r.URL.Query().Get("flash"),
			FlashError: r.URL.Query().Get("error") == "1",
		},
		TypeName:       typeName,
		ReadOnly:       ctx.ReadOnly,
		DisplayField:   typeCfg.DisplayField,
		PrimaryHeading: primaryHeading,
		ExtraFields:    extraFields,
		Items:          items,
		TypeConfigURL:  "/w/" + url.PathEscape(workspace) + "/config/types/" + url.PathEscape(typeName),
		NewItemURL:     "/w/" + url.PathEscape(workspace) + "/types/" + url.PathEscape(typeName) + "/new",
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
	fields := schemaToFieldData(schema)
	markUniqueFields(fields, ctx.Constraints, typeName)
	s.enrichForeignKeys(&ctx, typeName, fields)

	crumbID := firstNonEmpty(id, "new")
	crumbLabel := crumbID
	typeCfg := ctx.UI.Types[typeName]

	data := objectPageData{
		pageBase: pageBase{
			Top:        s.topBar(ctx, r.URL.Path),
			Crumbs:     buildCrumbsWithLabels(workspace, map[string]string{crumbID: crumbLabel}, typeName, crumbID),
			Flash:      r.URL.Query().Get("flash"),
			FlashError: r.URL.Query().Get("error") == "1",
		},
		TypeName:    typeName,
		ID:          id,
		ReadOnly:    ctx.ReadOnly,
		Fields:      fields,
		FieldValues: map[string]string{},
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
	// Update breadcrumb to use display field value
	if displayLabel := displayValue(obj.Data, typeCfg.DisplayField, ""); displayLabel != "" && displayLabel != id {
		data.Crumbs = buildCrumbsWithLabels(workspace, map[string]string{id: displayLabel}, typeName, id)
	}
	ensureForeignKeyCurrentOptions(data.Fields, data.FieldValues)
	if workspace != "main" {
		if mainObj, err := ReadObject(s.repo.Root, typeName, id); err == nil {
			data.Diffs = computeDiffs(mainObj.Data, obj.Data)
		}
	}
	data.InvalidIssues = ctx.ObjectIssues[typeName][id]
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
			Top: s.topBar(ctx, r.URL.Path),
			Crumbs: []breadcrumb{
				{Label: "Types", URL: "/w/" + url.PathEscape(workspace) + "/types"},
				{Label: "New Workspace", URL: r.URL.Path, Current: true},
			},
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

func (s *webServer) handleSaveConfirmPage(w http.ResponseWriter, r *http.Request, workspace string) {
	if workspace == "main" {
		s.redirectWithFlash(w, r, "/w/main/types", "main is read-only", true)
		return
	}
	ctx, err := s.loadContext(workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	entries, err := s.repo.ChangedEntries(ctx.RepoPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	changes := make([]confirmChange, 0, len(entries))
	for _, e := range entries {
		changes = append(changes, confirmChange{File: e.Path, Status: e.Status})
	}
	data := confirmSavePageData{
		pageBase: pageBase{
			Top: s.topBar(ctx, r.URL.Path),
			Crumbs: []breadcrumb{
				{Label: "Types", URL: "/w/" + url.PathEscape(workspace) + "/types"},
				{Label: "Save", URL: r.URL.Path, Current: true},
			},
		},
		Workspace: workspace,
		Changes:   changes,
		PostURL:   "/w/" + url.PathEscape(workspace) + "/save",
		BackURL:   "/w/" + url.PathEscape(workspace) + "/types",
	}
	s.renderTemplate(w, "confirm_save.html", data)
}

func (s *webServer) handleMergeConfirmPage(w http.ResponseWriter, r *http.Request, workspace string) {
	if workspace == "main" {
		s.redirectWithFlash(w, r, "/w/main/types", "main cannot be merged", true)
		return
	}
	ctx, err := s.loadContext(workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	branch := s.repo.BranchForWorkspace(workspace)
	changedFiles, err := s.repo.DiffWorkspaceDataFiles(branch)
	if err != nil {
		s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/types", err.Error(), true)
		return
	}
	changes := make([]confirmChange, 0, len(changedFiles))
	for _, f := range changedFiles {
		changes = append(changes, confirmChange{File: f, Status: "M"})
	}
	data := confirmMergePageData{
		pageBase: pageBase{
			Top: s.topBar(ctx, r.URL.Path),
			Crumbs: []breadcrumb{
				{Label: "Types", URL: "/w/" + url.PathEscape(workspace) + "/types"},
				{Label: "Merge", URL: r.URL.Path, Current: true},
			},
		},
		Workspace: workspace,
		Changes:   changes,
		PostURL:   "/w/" + url.PathEscape(workspace) + "/merge",
		BackURL:   "/w/" + url.PathEscape(workspace) + "/types",
	}
	s.renderTemplate(w, "confirm_merge.html", data)
}

func (s *webServer) handleWorkspaceMerge(w http.ResponseWriter, r *http.Request, workspace string) {
	if workspace == "main" {
		s.redirectWithFlash(w, r, "/w/main/types", "main cannot be merged", true)
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
			pageBase: pageBase{
				Top: s.topBar(ctx, r.URL.Path),
				Crumbs: []breadcrumb{
					{Label: "Types", URL: "/w/" + url.PathEscape(workspace) + "/types"},
					{Label: "Merge", URL: r.URL.Path, Current: true},
				},
			},
			Workspace: workspace,
			Conflicts: rows,
			PostURL:   "/w/" + url.PathEscape(workspace) + "/merge",
			BackURL:   returnPath,
		}
		s.renderTemplate(w, "promote_conflicts.html", data)
		return
	}
	s.redirectWithFlash(w, r, "/w/main/types", "Workspace merged to main", false)
}

func (s *webServer) handleWorkspaceValidate(w http.ResponseWriter, r *http.Request, workspace string) {
	ctx, err := s.loadContext(workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	returnPath := firstNonEmpty(r.FormValue("return"), "/w/"+url.PathEscape(workspace)+"/types")

	if ctx.ReadOnly {
		// On main: validate directly
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
		return
	}

	// On workspace: simulate merge then validate
	result, err := s.repo.ValidateMergePreview(workspace)
	if err != nil {
		s.redirectWithFlash(w, r, returnPath, err.Error(), true)
		return
	}
	if !result.OK() {
		s.redirectWithFlash(w, r, returnPath, result.Issues[0].String(), true)
		return
	}
	s.redirectWithFlash(w, r, returnPath, "Validation passed (including merge preview)", false)
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
	schemaLinks := make([]typeSettingLink, 0, len(types))
	for _, t := range types {
		links = append(links, typeSettingLink{TypeName: t, URL: "/w/" + url.PathEscape(workspace) + "/config/types/" + url.PathEscape(t)})
		schemaLinks = append(schemaLinks, typeSettingLink{TypeName: t, URL: "/w/" + url.PathEscape(workspace) + "/config/schemas/edit/" + url.PathEscape(t)})
	}
	data := configPageData{
		pageBase: pageBase{
			Top: s.topBar(ctx, r.URL.Path),
			Crumbs: []breadcrumb{
				{Label: "Types", URL: "/w/" + url.PathEscape(workspace) + "/types"},
				{Label: "Config", URL: "/w/" + url.PathEscape(workspace) + "/config", Current: true},
			},
			Flash:      r.URL.Query().Get("flash"),
			FlashError: r.URL.Query().Get("error") == "1",
		},
		ReadOnly:       ctx.ReadOnly,
		RepoName:       ctx.UI.RepoName,
		SaveURL:        "/w/" + url.PathEscape(workspace) + "/config",
		TypeSettings:   links,
		SchemaLinks:    schemaLinks,
		ConstraintsURL: "/w/" + url.PathEscape(workspace) + "/config/constraints",
		NewSchemaURL:   "/w/" + url.PathEscape(workspace) + "/config/schemas/new/new",
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
	selectedOrder := map[string]int{}
	for i, f := range tc.Fields {
		selectedOrder[f] = i + 1
	}
	extraOptions := make([]extraOption, 0, len(extraOrder))
	for i, field := range extraOrder {
		orderValue := 100 + i
		if v, ok := selectedOrder[field]; ok {
			orderValue = v
		}
		extraOptions = append(extraOptions, extraOption{Name: field, Checked: contains(tc.Fields, field), Order: orderValue})
	}

	data := typeConfigPageData{
		pageBase: pageBase{
			Top: s.topBar(ctx, r.URL.Path),
			Crumbs: []breadcrumb{
				{Label: "Types", URL: "/w/" + url.PathEscape(workspace) + "/types"},
				{Label: "Config", URL: "/w/" + url.PathEscape(workspace) + "/config"},
				{Label: typeName, URL: r.URL.Path, Current: true},
			},
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
	selected := dedupeOrdered(r.Form["extraField"])
	tc.Fields = sortSelectedFieldsByOrder(selected, r.Form)
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

func (s *webServer) handleSchemaEditPage(w http.ResponseWriter, r *http.Request, workspace, action, typeName string) {
	ctx, err := s.loadContext(workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	content := ""
	if typeName != "new" {
		schemaPath := filepath.Join(ctx.RepoPath, "config", "schemas", typeName+".schema.json")
		b, err := os.ReadFile(schemaPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		content = string(b)
	} else {
		content = `{
  "type": "object",
  "required": [],
  "properties": {}
}
`
	}
	data := schemaEditPageData{
		pageBase: pageBase{
			Top: s.topBar(ctx, r.URL.Path),
			Crumbs: []breadcrumb{
				{Label: "Types", URL: "/w/" + url.PathEscape(workspace) + "/types"},
				{Label: "Config", URL: "/w/" + url.PathEscape(workspace) + "/config"},
				{Label: "Schema: " + typeName, URL: r.URL.Path, Current: true},
			},
			Flash:      r.URL.Query().Get("flash"),
			FlashError: r.URL.Query().Get("error") == "1",
		},
		ReadOnly: ctx.ReadOnly,
		TypeName: typeName,
		Content:  content,
		SaveURL:  "/w/" + url.PathEscape(workspace) + "/config/schemas/" + url.PathEscape(action) + "/" + url.PathEscape(typeName),
		BackURL:  "/w/" + url.PathEscape(workspace) + "/config",
	}
	s.renderTemplate(w, "schema_edit.html", data)
}

func (s *webServer) handleSchemaEditSave(w http.ResponseWriter, r *http.Request, workspace, action, typeName string) {
	ctx, err := s.loadContext(workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if ctx.ReadOnly {
		s.redirectWithFlash(w, r, "/w/main/config", "main is read-only", true)
		return
	}
	content := r.FormValue("content")
	newTypeName := strings.TrimSpace(r.FormValue("typeName"))

	if action == "new" {
		if newTypeName == "" {
			s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/config/schemas/new/new", "type name is required", true)
			return
		}
		typeName = newTypeName
	}

	// Validate the JSON schema content
	if err := ValidateSchemaContent([]byte(content), typeName); err != nil {
		redirectURL := "/w/" + url.PathEscape(workspace) + "/config/schemas/" + url.PathEscape(action) + "/" + url.PathEscape(typeName)
		s.redirectWithFlash(w, r, redirectURL, err.Error(), true)
		return
	}

	schemaPath := filepath.Join(ctx.RepoPath, "config", "schemas", typeName+".schema.json")
	if err := os.MkdirAll(filepath.Dir(schemaPath), 0o755); err != nil {
		s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/config", err.Error(), true)
		return
	}
	if err := os.WriteFile(schemaPath, []byte(content), 0o644); err != nil {
		s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/config", err.Error(), true)
		return
	}

	// Ensure data directory exists for the type
	dataDir := filepath.Join(ctx.RepoPath, "data", typeName)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/config", err.Error(), true)
		return
	}

	s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/config", "Schema for "+typeName+" updated", false)
}

func (s *webServer) handleConstraintsEditPage(w http.ResponseWriter, r *http.Request, workspace string) {
	ctx, err := s.loadContext(workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	content := ""
	constraintPath := filepath.Join(ctx.RepoPath, "config", "constraints.json")
	if b, err := os.ReadFile(constraintPath); err == nil {
		content = string(b)
	} else {
		content = `{
  "unique": [],
  "foreignKeys": []
}
`
	}
	data := constraintsEditPageData{
		pageBase: pageBase{
			Top: s.topBar(ctx, r.URL.Path),
			Crumbs: []breadcrumb{
				{Label: "Types", URL: "/w/" + url.PathEscape(workspace) + "/types"},
				{Label: "Config", URL: "/w/" + url.PathEscape(workspace) + "/config"},
				{Label: "Constraints", URL: r.URL.Path, Current: true},
			},
			Flash:      r.URL.Query().Get("flash"),
			FlashError: r.URL.Query().Get("error") == "1",
		},
		ReadOnly: ctx.ReadOnly,
		Content:  content,
		SaveURL:  "/w/" + url.PathEscape(workspace) + "/config/constraints",
		BackURL:  "/w/" + url.PathEscape(workspace) + "/config",
	}
	s.renderTemplate(w, "constraints_edit.html", data)
}

func (s *webServer) handleConstraintsEditSave(w http.ResponseWriter, r *http.Request, workspace string) {
	ctx, err := s.loadContext(workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if ctx.ReadOnly {
		s.redirectWithFlash(w, r, "/w/main/config", "main is read-only", true)
		return
	}
	content := r.FormValue("content")

	// Validate JSON parses as Constraints
	var c Constraints
	if err := json.Unmarshal([]byte(content), &c); err != nil {
		s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/config/constraints", "Invalid JSON: "+err.Error(), true)
		return
	}

	constraintPath := filepath.Join(ctx.RepoPath, "config", "constraints.json")
	if err := os.WriteFile(constraintPath, []byte(content), 0o644); err != nil {
		s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/config/constraints", err.Error(), true)
		return
	}
	s.redirectWithFlash(w, r, "/w/"+url.PathEscape(workspace)+"/config", "Constraints updated", false)
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
		fields = append(fields, fieldData{
			Name:      name,
			Type:      prop.Type,
			ItemsType: prop.ItemsType,
			Required:  required,
			Enum:      prop.Enum,
			MinLength: intPtrString(prop.MinLength),
			MaxLength: intPtrString(prop.MaxLength),
			Minimum:   floatPtrString(prop.Minimum),
			Maximum:   floatPtrString(prop.Maximum),
		})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	return fields
}

func markUniqueFields(fields []fieldData, constraints Constraints, typeName string) {
	for i := range fields {
		for _, uc := range constraints.Unique {
			if uc.Type == typeName && uc.Field == fields[i].Name {
				fields[i].Unique = true
				break
			}
		}
	}
}

func (s *webServer) enrichForeignKeys(ctx *workspaceContext, typeName string, fields []fieldData) {
	if ctx == nil || len(fields) == 0 || len(ctx.Constraints.ForeignKeys) == 0 {
		return
	}
	for i := range fields {
		fieldName := fields[i].Name
		var constraint *ForeignKeyConstraint
		for j := range ctx.Constraints.ForeignKeys {
			fk := &ctx.Constraints.ForeignKeys[j]
			if fk.FromType == typeName && fk.FromField == fieldName {
				constraint = fk
				break
			}
		}
		if constraint == nil {
			continue
		}
		displayField := constraint.ToDisplayField
		if strings.TrimSpace(displayField) == "" {
			// Fall back to the UI config display field for the target type
			if targetCfg, ok := ctx.UI.Types[constraint.ToType]; ok && targetCfg.DisplayField != "" && targetCfg.DisplayField != "_id" {
				displayField = targetCfg.DisplayField
			} else {
				displayField = constraint.ToField
			}
		}

		targets, err := ListObjectsForType(ctx.RepoPath, constraint.ToType)
		if err != nil {
			continue
		}

		options := make([]foreignKeyOption, 0, len(targets))
		seenValues := map[string]struct{}{}
		for _, target := range targets {
			stored, ok := target.Data[constraint.ToField]
			if !ok || stored == nil {
				continue
			}
			value := valueToForm(stored)
			if strings.TrimSpace(value) == "" {
				continue
			}
			if _, ok := seenValues[value]; ok {
				continue
			}
			seenValues[value] = struct{}{}

			label := ""
			if displayField == "_id" {
				label = target.ID
			} else if rawDisplay, ok := target.Data[displayField]; ok && rawDisplay != nil {
				label = valueToText(rawDisplay)
			}
			if strings.TrimSpace(label) == "" {
				label = value
			}
			options = append(options, foreignKeyOption{Value: value, Display: label})
		}

		sort.Slice(options, func(a, b int) bool {
			if options[a].Display == options[b].Display {
				return options[a].Value < options[b].Value
			}
			return options[a].Display < options[b].Display
		})

		fields[i].ForeignKey = &foreignKeyField{
			ValueField:   constraint.ToField,
			DisplayField: displayField,
			Options:      options,
		}
	}
}

func ensureForeignKeyCurrentOptions(fields []fieldData, values map[string]string) {
	if len(fields) == 0 || len(values) == 0 {
		return
	}
	for i := range fields {
		fk := fields[i].ForeignKey
		if fk == nil {
			continue
		}
		current := strings.TrimSpace(values[fields[i].Name])
		if current == "" {
			continue
		}
		found := false
		for _, option := range fk.Options {
			if option.Value == current {
				found = true
				break
			}
		}
		if found {
			continue
		}
		fk.Options = append(fk.Options, foreignKeyOption{
			Value:   current,
			Display: current + " (missing)",
		})
	}
}

func parseFormField(raw string, prop SchemaProperty) (any, error) {
	switch prop.Type {
	case "string":
		return raw, nil
	case "number", "integer":
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			// Keep invalid numeric drafts as strings; save/validate will catch.
			return raw, nil
		}
		return n, nil
	case "boolean":
		if raw == "true" {
			return true, nil
		}
		if raw == "false" {
			return false, nil
		}
		return raw, nil
	case "array":
		parts := strings.Split(raw, ",")
		arr := make([]any, 0, len(parts))
		rawParts := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			rawParts = append(rawParts, p)
			if prop.ItemsType == "string" {
				arr = append(arr, p)
			} else {
				n, err := strconv.ParseFloat(p, 64)
				if err != nil {
					// Convert to all-strings array to preserve parseability.
					strArr := make([]any, 0, len(rawParts))
					for _, sp := range rawParts {
						strArr = append(strArr, sp)
					}
					return strArr, nil
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

func buildCrumbs(workspace string, parts ...string) []breadcrumb {
	return buildCrumbsWithLabels(workspace, nil, parts...)
}

func buildCrumbsWithLabels(workspace string, labels map[string]string, parts ...string) []breadcrumb {
	crumbs := []breadcrumb{
		{
			Label: "Types",
			URL:   "/w/" + url.PathEscape(workspace) + "/types",
		},
	}
	if len(parts) == 0 {
		crumbs[0].Current = true
		return crumbs
	}
	accumulated := crumbs[0].URL
	for i, part := range parts {
		accumulated += "/" + url.PathEscape(part)
		label := part
		if labels != nil {
			if l, ok := labels[part]; ok && l != "" {
				label = l
			}
		}
		crumbs = append(crumbs, breadcrumb{
			Label: label,
			URL:   accumulated,
		})
		if i == len(parts)-1 {
			crumbs[len(crumbs)-1].Current = true
		}
	}
	crumbs[0].Current = len(parts) == 0
	return crumbs
}

func collectObjectIssues(repoPath string) (map[string]map[string][]ValidationIssue, error) {
	result := map[string]map[string][]ValidationIssue{}
	validation, err := ValidateRepository(repoPath)
	if err != nil {
		return result, err
	}
	for _, issue := range validation.Issues {
		typeName, id, ok := parseDataObjectPath(issue.Path)
		if !ok {
			continue
		}
		if _, ok := result[typeName]; !ok {
			result[typeName] = map[string][]ValidationIssue{}
		}
		result[typeName][id] = append(result[typeName][id], issue)
	}
	return result, nil
}

func sortSelectedFieldsByOrder(selected []string, form url.Values) []string {
	type row struct {
		field string
		order int
	}
	rows := make([]row, 0, len(selected))
	for i, field := range selected {
		raw := strings.TrimSpace(form.Get("order." + field))
		order := 100 + i
		if raw != "" {
			if v, err := strconv.Atoi(raw); err == nil {
				order = v
			}
		}
		rows = append(rows, row{field: field, order: order})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].order == rows[j].order {
			return rows[i].field < rows[j].field
		}
		return rows[i].order < rows[j].order
	})
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.field)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func intPtrString(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

func floatPtrString(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}
