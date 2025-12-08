package server

import (
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/estrados/simply-aws/internal/awscli"
	"github.com/estrados/simply-aws/internal/cfn"
	"github.com/estrados/simply-aws/internal/project"
	sawsSync "github.com/estrados/simply-aws/internal/sync"
	"github.com/estrados/simply-aws/web"
)

var (
	awsStatus awscli.Status
	tmpl      *template.Template
)

func Start(addr string, status awscli.Status) error {
	awsStatus = status

	funcMap := template.FuncMap{
		"not": func(b bool) bool { return !b },
	}

	var err error
	tmpl, err = template.New("").Funcs(funcMap).ParseFS(web.Templates, "templates/*.html")
	if err != nil {
		return err
	}

	mux := http.NewServeMux()

	// Static assets
	staticFS, _ := fs.Sub(web.Static, ".")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Pages
	mux.HandleFunc("/", handleHome)
	mux.HandleFunc("/settings", handleSettings)
	mux.HandleFunc("/settings/regions/", handleRegionToggle)

	// JSON APIs (kept for sync/templates)
	mux.HandleFunc("/api/status", handleAPIStatus)
	mux.HandleFunc("/api/templates", handleAPITemplates)
	mux.HandleFunc("/api/resources", handleAPIResources)
	mux.HandleFunc("/api/sync", handleAPISync)
	mux.HandleFunc("/api/aws/", handleAPIAWSCache)

	return http.ListenAndServe(addr, mux)
}

type pageData struct {
	CurrentRegion  string
	EnabledRegions []string
	Regions        []sawsSync.RegionInfo
}

func newPageData() pageData {
	enabled, _ := sawsSync.GetEnabledRegions()
	return pageData{
		CurrentRegion:  awsStatus.Region,
		EnabledRegions: enabled,
	}
}

func handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	ensureRegionsSeeded()
	data := newPageData()
	tmpl.ExecuteTemplate(w, "layout", data)
}

func handleSettings(w http.ResponseWriter, r *http.Request) {
	ensureRegionsSeeded()
	regions, _ := sawsSync.GetRegions()
	data := newPageData()
	data.Regions = regions
	tmpl.ExecuteTemplate(w, "settings", data)
}

func handleRegionToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "use PUT", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/settings/regions/")
	enabled := r.URL.Query().Get("enabled") == "true"

	if name == "all" {
		allRegions, _ := sawsSync.GetRegions()
		for _, reg := range allRegions {
			sawsSync.SetRegionEnabled(reg.Name, enabled)
		}
	} else {
		sawsSync.SetRegionEnabled(name, enabled)
	}

	// Re-render the region list + update the dropdown via OOB swap
	regions, _ := sawsSync.GetRegions()
	tmpl.ExecuteTemplate(w, "region-list", regions)

	// OOB swap for the dropdown
	data := newPageData()
	w.Write([]byte(`<div id="region-select-wrapper" hx-swap-oob="innerHTML">`))
	tmpl.ExecuteTemplate(w, "region-dropdown", data)
	w.Write([]byte(`</div>`))
}

func ensureRegionsSeeded() {
	regions, _ := sawsSync.GetRegions()
	if len(regions) > 0 {
		return
	}
	if !awsStatus.Installed {
		return
	}
	data, err := awscli.Run("ec2", "describe-regions", "--all-regions",
		"--query", "Regions[?OptInStatus!='not-opted-in'].[RegionName]", "--output", "json")
	if err != nil {
		return
	}
	var nested [][]string
	json.Unmarshal(data, &nested)
	var names []string
	for _, r := range nested {
		if len(r) > 0 {
			names = append(names, r[0])
		}
	}
	sawsSync.SetRegions(names)
}

// --- JSON API handlers (unchanged) ---

func handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	lastSync, _ := sawsSync.ReadLastSync()
	writeJSON(w, map[string]interface{}{
		"aws":      awsStatus,
		"lastSync": lastSync,
	})
}

func handleAPITemplates(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	cwd, _ := os.Getwd()
	templates, err := project.ScanTemplates(cwd)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if file != "" {
		for _, t := range templates {
			if t.File == file {
				writeJSON(w, t)
				return
			}
		}
		http.Error(w, "template not found", 404)
		return
	}
	type summary struct {
		File          string   `json:"file"`
		Description   string   `json:"description,omitempty"`
		ResourceCount int      `json:"resourceCount"`
		ResourceTypes []string `json:"resourceTypes"`
	}
	var list []summary
	for _, t := range templates {
		list = append(list, summary{
			File:          t.File,
			Description:   t.Description,
			ResourceCount: len(t.Resources),
			ResourceTypes: resourceTypes(t),
		})
	}
	writeJSON(w, list)
}

func handleAPIResources(w http.ResponseWriter, r *http.Request) {
	cwd, _ := os.Getwd()
	templates, err := project.ScanTemplates(cwd)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	type resource struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Template string `json:"template"`
	}
	var all []resource
	for _, t := range templates {
		for name, res := range t.Resources {
			all = append(all, resource{Name: name, Type: res.Type, Template: t.File})
		}
	}
	writeJSON(w, all)
}

func handleAPISync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	if !awsStatus.Installed {
		http.Error(w, "AWS CLI not available", http.StatusServiceUnavailable)
		return
	}
	results, err := sawsSync.SyncAll()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, results)
}

func handleAPIAWSCache(w http.ResponseWriter, r *http.Request) {
	service := strings.TrimPrefix(r.URL.Path, "/api/aws/")
	service = filepath.Clean(service)
	if service == "" || service == "." {
		validServices := []string{"vpc", "ec2", "ecs", "rds", "s3", "cloudformation"}
		type serviceInfo struct {
			Name   string `json:"name"`
			Cached bool   `json:"cached"`
		}
		var list []serviceInfo
		for _, s := range validServices {
			list = append(list, serviceInfo{Name: s, Cached: sawsSync.CacheExists(s)})
		}
		writeJSON(w, list)
		return
	}
	data, err := sawsSync.ReadCache(service)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if data == nil {
		writeJSON(w, nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func resourceTypes(t *cfn.Template) []string {
	seen := map[string]bool{}
	var types []string
	for _, r := range t.Resources {
		if !seen[r.Type] {
			seen[r.Type] = true
			types = append(types, r.Type)
		}
	}
	return types
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
