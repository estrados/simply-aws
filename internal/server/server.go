package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/estrados/simply-aws/internal/awscli"
	"github.com/estrados/simply-aws/internal/cfn"
	"github.com/estrados/simply-aws/internal/project"
	"github.com/estrados/simply-aws/internal/sync"
	"github.com/estrados/simply-aws/web"
)

var awsStatus awscli.Status

func Start(addr string, status awscli.Status) error {
	awsStatus = status

	mux := http.NewServeMux()

	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/regions", handleRegions)
	mux.HandleFunc("/api/regions/", handleRegionToggle)
	mux.HandleFunc("/api/templates", handleTemplates)
	mux.HandleFunc("/api/resources", handleResources)
	mux.HandleFunc("/api/sync", handleSync)
	mux.HandleFunc("/api/aws/", handleAWSCache)

	mux.Handle("/", web.Handler())

	return http.ListenAndServe(addr, mux)
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	lastSync, _ := sync.ReadLastSync()
	resp := map[string]interface{}{
		"aws":      awsStatus,
		"lastSync": lastSync,
	}
	writeJSON(w, resp)
}

func handleRegions(w http.ResponseWriter, r *http.Request) {
	// If we have regions in DB, return them
	regions, _ := sync.GetRegions()
	if len(regions) > 0 {
		writeJSON(w, regions)
		return
	}

	// First time: fetch from AWS CLI and seed the DB
	if !awsStatus.Installed {
		writeJSON(w, []sync.RegionInfo{})
		return
	}

	data, err := awscli.Run("ec2", "describe-regions", "--all-regions",
		"--query", "Regions[?OptInStatus!='not-opted-in'].[RegionName]", "--output", "json")
	if err != nil {
		http.Error(w, err.Error(), 500)
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

	sync.SetRegions(names)

	regions, _ = sync.GetRegions()
	writeJSON(w, regions)
}

// PUT /api/regions/{name} with body {"enabled": true/false}
func handleRegionToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "use PUT", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/regions/")
	if name == "" {
		http.Error(w, "missing region name", 400)
		return
	}

	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	if err := sync.SetRegionEnabled(name, body.Enabled); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func handleTemplates(w http.ResponseWriter, r *http.Request) {
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
		types := resourceTypes(t)
		list = append(list, summary{
			File:          t.File,
			Description:   t.Description,
			ResourceCount: len(t.Resources),
			ResourceTypes: types,
		})
	}
	writeJSON(w, list)
}

func handleResources(w http.ResponseWriter, r *http.Request) {
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
			all = append(all, resource{
				Name:     name,
				Type:     res.Type,
				Template: t.File,
			})
		}
	}
	writeJSON(w, all)
}

func handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	if !awsStatus.Installed {
		http.Error(w, "AWS CLI not available", http.StatusServiceUnavailable)
		return
	}

	results, err := sync.SyncAll()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, results)
}

func handleAWSCache(w http.ResponseWriter, r *http.Request) {
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
			list = append(list, serviceInfo{
				Name:   s,
				Cached: sync.CacheExists(s),
			})
		}
		writeJSON(w, list)
		return
	}

	data, err := sync.ReadCache(service)
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
