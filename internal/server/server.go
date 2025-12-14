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
		"not":           func(b bool) bool { return !b },
		"regionDisplay": awscli.RegionDisplayName,
		"hasVPCData": func(v *sawsSync.VPCData) bool {
			return v != nil && len(v.VPCs) > 0
		},
		"subnetsFor": func(vpcId string, data *sawsSync.VPCData) []sawsSync.Subnet {
			var out []sawsSync.Subnet
			for _, s := range data.Subnets {
				if s.VpcId == vpcId {
					out = append(out, s)
				}
			}
			return out
		},
		"igwsFor": func(vpcId string, data *sawsSync.VPCData) []sawsSync.IGW {
			var out []sawsSync.IGW
			for _, g := range data.IGWs {
				for _, id := range g.AttachedVpcIds {
					if id == vpcId {
						out = append(out, g)
						break
					}
				}
			}
			return out
		},
		"natgwsFor": func(vpcId string, data *sawsSync.VPCData) []sawsSync.NATGW {
			var out []sawsSync.NATGW
			for _, n := range data.NATGWs {
				if n.VpcId == vpcId {
					out = append(out, n)
				}
			}
			return out
		},
		"hasIGWRoute": func(rt sawsSync.RouteTable) bool {
			for _, r := range rt.Routes {
				if strings.HasPrefix(r.GatewayId, "igw-") {
					return true
				}
			}
			return false
		},
		"rtAccess": func(rt sawsSync.RouteTable) string {
			for _, r := range rt.Routes {
				if strings.HasPrefix(r.GatewayId, "igw-") {
					return "public"
				}
			}
			for _, r := range rt.Routes {
				if strings.HasPrefix(r.NatGatewayId, "nat-") {
					return "egress-only"
				}
			}
			return "isolated"
		},
		"sgsFor": func(vpcId string, data *sawsSync.VPCData) []sawsSync.SecurityGroup {
			var out []sawsSync.SecurityGroup
			for _, sg := range data.SecurityGroups {
				if sg.VpcId == vpcId {
					out = append(out, sg)
				}
			}
			return out
		},
		"routeTablesFor": func(vpcId string, data *sawsSync.VPCData) []sawsSync.RouteTable {
			var out []sawsSync.RouteTable
			for _, r := range data.RouteTables {
				if r.VpcId == vpcId {
					out = append(out, r)
				}
			}
			return out
		},
		"subnetsForRT": func(rt sawsSync.RouteTable, vpcId string, data *sawsSync.VPCData) []sawsSync.Subnet {
			if rt.IsMain {
				// Main RT gets all subnets not explicitly associated to another RT
				explicit := map[string]bool{}
				for _, r := range data.RouteTables {
					if r.VpcId == vpcId && !r.IsMain {
						for _, sid := range r.SubnetIds {
							explicit[sid] = true
						}
					}
				}
				var out []sawsSync.Subnet
				for _, s := range data.Subnets {
					if s.VpcId == vpcId && !explicit[s.SubnetId] {
						out = append(out, s)
					}
				}
				return out
			}
			// Non-main RT: return explicitly associated subnets
			ids := map[string]bool{}
			for _, sid := range rt.SubnetIds {
				ids[sid] = true
			}
			var out []sawsSync.Subnet
			for _, s := range data.Subnets {
				if ids[s.SubnetId] {
					out = append(out, s)
				}
			}
			return out
		},
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
	mux.HandleFunc("/settings/regions", handleRegionSettings)
	mux.HandleFunc("/settings/regions/", handleRegionToggle)
	mux.HandleFunc("/profile", handleProfile)
	mux.HandleFunc("/vpc", handleVPC)
	mux.HandleFunc("/sync/vpc", handleSyncVPC)

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
	AWS            awscli.Status
	Region         string
	VPC            *sawsSync.VPCData
}

func newPageData() pageData {
	enabled, _ := sawsSync.GetEnabledRegions()
	return pageData{
		CurrentRegion:  awsStatus.Region,
		EnabledRegions: enabled,
		AWS:            awsStatus,
	}
}

func handleHome(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")

	// Known routes — skip
	for _, prefix := range []string{"static", "settings", "profile", "vpc", "sync", "api"} {
		if strings.HasPrefix(path, prefix) {
			http.NotFound(w, r)
			return
		}
	}

	ensureRegionsSeeded()

	// / → redirect to /{default-region}
	if path == "" {
		region := awsStatus.Region
		if region == "" {
			enabled, _ := sawsSync.GetEnabledRegions()
			if len(enabled) > 0 {
				region = enabled[0]
			}
		}
		if region != "" {
			http.Redirect(w, r, "/"+region, http.StatusFound)
			return
		}
	}

	// /{region} → render page with that region
	region := path
	data := newPageData()
	data.CurrentRegion = region
	data.Region = region

	vpcData, _ := sawsSync.LoadVPCData(region)
	data.VPC = vpcData

	tmpl.ExecuteTemplate(w, "layout", data)
}

func handleRegionSettings(w http.ResponseWriter, r *http.Request) {
	ensureRegionsSeeded()
	regions, _ := sawsSync.GetRegions()
	data := newPageData()
	data.Regions = regions
	tmpl.ExecuteTemplate(w, "region-settings", data)
}

func handleProfile(w http.ResponseWriter, r *http.Request) {
	data := newPageData()
	tmpl.ExecuteTemplate(w, "profile", data)
}

func handleVPC(w http.ResponseWriter, r *http.Request) {
	region := r.URL.Query().Get("region")
	if region == "" {
		region = awsStatus.Region
	}
	vpcData, _ := sawsSync.LoadVPCData(region)
	data := newPageData()
	data.Region = region
	data.VPC = vpcData
	tmpl.ExecuteTemplate(w, "vpc-panel", data)
}

func handleSyncVPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	r.ParseForm()
	region := r.FormValue("region")
	if region == "" {
		region = awsStatus.Region
	}
	sawsSync.SyncVPCData(region)
	vpcData, _ := sawsSync.LoadVPCData(region)
	data := newPageData()
	data.Region = region
	data.VPC = vpcData
	tmpl.ExecuteTemplate(w, "vpc-panel", data)
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
