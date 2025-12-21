package server

import (
	"encoding/json"
	"fmt"
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
	mux.HandleFunc("/detail/", handleDetail)

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
	Tab            string
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
	for _, prefix := range []string{"static", "settings", "profile", "vpc", "sync", "api", "detail"} {
		if strings.HasPrefix(path, prefix) {
			http.NotFound(w, r)
			return
		}
	}

	ensureRegionsSeeded()

	// / → redirect to /{default-region}/net
	if path == "" {
		region := awsStatus.Region
		if region == "" {
			enabled, _ := sawsSync.GetEnabledRegions()
			if len(enabled) > 0 {
				region = enabled[0]
			}
		}
		if region != "" {
			http.Redirect(w, r, "/"+region+"/net", http.StatusFound)
			return
		}
	}

	// Parse /{region} or /{region}/{tab}
	parts := strings.SplitN(path, "/", 2)
	region := parts[0]
	tab := "net"
	if len(parts) == 2 && parts[1] != "" {
		tab = parts[1]
	}

	// /{region} without tab → redirect to /{region}/net
	if len(parts) == 1 || parts[1] == "" {
		http.Redirect(w, r, "/"+region+"/net", http.StatusFound)
		return
	}

	validTabs := map[string]bool{"net": true, "compute": true, "database": true, "s3": true}
	if !validTabs[tab] {
		http.NotFound(w, r)
		return
	}

	data := newPageData()
	data.CurrentRegion = region
	data.Region = region
	data.Tab = tab

	if tab == "net" {
		vpcData, _ := sawsSync.LoadVPCData(region)
		data.VPC = vpcData
	}

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

type detailData struct {
	Type          string
	Title         string
	Fields        []detailField
	Rules         [][]string
	RulesTitle    string
	Outbound      [][]string
	OutboundTitle string
	Routes        [][]string
}

type detailField struct {
	Label string
	Value string
}

// GET /detail/{type}/{id}?region=xxx
func handleDetail(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/detail/"), "/", 2)
	if len(parts) != 2 {
		http.Error(w, "bad path", 400)
		return
	}
	resType, resId := parts[0], parts[1]
	region := r.URL.Query().Get("region")
	if region == "" {
		region = awsStatus.Region
	}

	vpcData, _ := sawsSync.LoadVPCData(region)
	if vpcData == nil {
		http.Error(w, "no data", 404)
		return
	}

	var detail detailData

	switch resType {
	case "vpc":
		for _, v := range vpcData.VPCs {
			if v.VpcId == resId {
				subnets := 0
				for _, s := range vpcData.Subnets {
					if s.VpcId == v.VpcId {
						subnets++
					}
				}
				sgs := 0
				for _, sg := range vpcData.SecurityGroups {
					if sg.VpcId == v.VpcId {
						sgs++
					}
				}
				detail = detailData{
					Type:  "VPC",
					Title: nameOr(v.Name, v.VpcId),
					Fields: []detailField{
						{"VPC ID", v.VpcId},
						{"CIDR Block", v.CidrBlock},
						{"State", v.State},
						{"Default", boolStr(v.IsDefault)},
						{"Subnets", fmt.Sprintf("%d", subnets)},
						{"Security Groups", fmt.Sprintf("%d", sgs)},
					},
				}
				break
			}
		}
	case "subnet":
		for _, s := range vpcData.Subnets {
			if s.SubnetId == resId {
				detail = detailData{
					Type:  "SUBNET",
					Title: nameOr(s.Name, s.SubnetId),
					Fields: []detailField{
						{"Subnet ID", s.SubnetId},
						{"VPC ID", s.VpcId},
						{"CIDR Block", s.CidrBlock},
						{"Availability Zone", s.AvailabilityZone},
						{"State", s.State},
						{"Available IPs", fmt.Sprintf("%d", s.AvailableIPs)},
					},
				}
				break
			}
		}
	case "sg":
		for _, sg := range vpcData.SecurityGroups {
			if sg.GroupId == resId {
				inbound, outbound := loadSGRules(region, resId)
				detail = detailData{
					Type:  "SG",
					Title: nameOr(sg.Name, sg.GroupName),
					Fields: []detailField{
						{"Group ID", sg.GroupId},
						{"Group Name", sg.GroupName},
						{"VPC ID", sg.VpcId},
						{"Description", sg.Description},
						{"Inbound Rules", fmt.Sprintf("%d", sg.InboundCount)},
						{"Outbound Rules", fmt.Sprintf("%d", sg.OutboundCount)},
					},
					RulesTitle:    "Inbound Rules",
					Rules:         inbound,
					OutboundTitle: "Outbound Rules",
					Outbound:      outbound,
				}
				break
			}
		}
	case "rt":
		for _, rt := range vpcData.RouteTables {
			if rt.RouteTableId == resId {
				access := "isolated"
				for _, route := range rt.Routes {
					if strings.HasPrefix(route.GatewayId, "igw-") {
						access = "public"
						break
					}
					if strings.HasPrefix(route.NatGatewayId, "nat-") {
						access = "egress-only"
					}
				}
				detail = detailData{
					Type:  "RT",
					Title: nameOr(rt.Name, rt.RouteTableId),
					Fields: []detailField{
						{"Route Table ID", rt.RouteTableId},
						{"VPC ID", rt.VpcId},
						{"Access Level", access},
						{"Main", boolStr(rt.IsMain)},
						{"Associated Subnets", fmt.Sprintf("%d", len(rt.SubnetIds))},
					},
				}
				for _, route := range rt.Routes {
					target := route.GatewayId
					if target == "" {
						target = route.NatGatewayId
					}
					if target == "" {
						target = "—"
					}
					detail.Routes = append(detail.Routes, []string{route.Destination, target, route.State})
				}
				break
			}
		}
	case "igw":
		for _, g := range vpcData.IGWs {
			if g.InternetGatewayId == resId {
				vpcs := strings.Join(g.AttachedVpcIds, ", ")
				if vpcs == "" {
					vpcs = "—"
				}
				detail = detailData{
					Type:  "IGW",
					Title: nameOr(g.Name, g.InternetGatewayId),
					Fields: []detailField{
						{"IGW ID", g.InternetGatewayId},
						{"Attached VPCs", vpcs},
					},
				}
				break
			}
		}
	case "natgw":
		for _, n := range vpcData.NATGWs {
			if n.NatGatewayId == resId {
				detail = detailData{
					Type:  "NAT",
					Title: nameOr(n.Name, n.NatGatewayId),
					Fields: []detailField{
						{"NAT Gateway ID", n.NatGatewayId},
						{"VPC ID", n.VpcId},
						{"Subnet ID", n.SubnetId},
						{"State", n.State},
					},
				}
				break
			}
		}
	}

	if detail.Type == "" {
		http.Error(w, "not found", 404)
		return
	}

	tmpl.ExecuteTemplate(w, "detail-panel", detail)
}

type sgPermission struct {
	IpProtocol string `json:"IpProtocol"`
	FromPort   *int   `json:"FromPort"`
	ToPort     *int   `json:"ToPort"`
	IpRanges   []struct {
		CidrIp      string `json:"CidrIp"`
		Description string `json:"Description"`
	} `json:"IpRanges"`
	Ipv6Ranges []struct {
		CidrIpv6    string `json:"CidrIpv6"`
		Description string `json:"Description"`
	} `json:"Ipv6Ranges"`
	UserIdGroupPairs []struct {
		GroupId     string `json:"GroupId"`
		Description string `json:"Description"`
	} `json:"UserIdGroupPairs"`
	PrefixListIds []struct {
		PrefixListId string `json:"PrefixListId"`
		Description  string `json:"Description"`
	} `json:"PrefixListIds"`
}

func parseSGPerms(perms []sgPermission) [][]string {
	var rules [][]string
	for _, perm := range perms {
		proto := perm.IpProtocol
		if proto == "-1" {
			proto = "All"
		}
		port := "All"
		if perm.FromPort != nil {
			if *perm.FromPort == *perm.ToPort {
				port = fmt.Sprintf("%d", *perm.FromPort)
			} else {
				port = fmt.Sprintf("%d-%d", *perm.FromPort, *perm.ToPort)
			}
		}
		for _, cidr := range perm.IpRanges {
			desc := cidr.Description
			if desc == "" {
				desc = "—"
			}
			rules = append(rules, []string{proto, port, cidr.CidrIp, desc})
		}
		for _, cidr := range perm.Ipv6Ranges {
			desc := cidr.Description
			if desc == "" {
				desc = "—"
			}
			rules = append(rules, []string{proto, port, cidr.CidrIpv6, desc})
		}
		for _, sg := range perm.UserIdGroupPairs {
			desc := sg.Description
			if desc == "" {
				desc = "—"
			}
			rules = append(rules, []string{proto, port, sg.GroupId, desc})
		}
		for _, pl := range perm.PrefixListIds {
			desc := pl.Description
			if desc == "" {
				desc = "—"
			}
			rules = append(rules, []string{proto, port, pl.PrefixListId, desc})
		}
	}
	return rules
}

func loadSGRules(region, sgId string) (inbound, outbound [][]string) {
	raw, err := sawsSync.ReadCache(region + ":security-groups")
	if err != nil || raw == nil {
		return nil, nil
	}
	var resp struct {
		SecurityGroups []json.RawMessage `json:"SecurityGroups"`
	}
	json.Unmarshal(raw, &resp)
	for _, sgRaw := range resp.SecurityGroups {
		var sg struct {
			GroupId             string         `json:"GroupId"`
			IpPermissions       []sgPermission `json:"IpPermissions"`
			IpPermissionsEgress []sgPermission `json:"IpPermissionsEgress"`
		}
		json.Unmarshal(sgRaw, &sg)
		if sg.GroupId != sgId {
			continue
		}
		return parseSGPerms(sg.IpPermissions), parseSGPerms(sg.IpPermissionsEgress)
	}
	return nil, nil
}

func nameOr(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

func boolStr(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
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
