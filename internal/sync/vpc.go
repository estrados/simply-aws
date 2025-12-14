package sync

import "encoding/json"

type VPCData struct {
	VPCs           []VPC           `json:"vpcs"`
	Subnets        []Subnet        `json:"subnets"`
	IGWs           []IGW           `json:"igws"`
	NATGWs         []NATGW         `json:"natGws"`
	RouteTables    []RouteTable    `json:"routeTables"`
	SecurityGroups []SecurityGroup `json:"securityGroups"`
}

type VPC struct {
	VpcId     string `json:"VpcId"`
	CidrBlock string `json:"CidrBlock"`
	State     string `json:"State"`
	IsDefault bool   `json:"IsDefault"`
	Name      string `json:"Name"`
}

type Subnet struct {
	SubnetId         string `json:"SubnetId"`
	VpcId            string `json:"VpcId"`
	CidrBlock        string `json:"CidrBlock"`
	AvailabilityZone string `json:"AvailabilityZone"`
	State            string `json:"State"`
	AvailableIPs     int    `json:"AvailableIpAddressCount"`
	Name             string `json:"Name"`
}

type IGW struct {
	InternetGatewayId string   `json:"InternetGatewayId"`
	AttachedVpcIds    []string `json:"AttachedVpcIds"`
	Name              string   `json:"Name"`
}

type NATGW struct {
	NatGatewayId string `json:"NatGatewayId"`
	VpcId        string `json:"VpcId"`
	SubnetId     string `json:"SubnetId"`
	State        string `json:"State"`
	Name         string `json:"Name"`
}

type RouteTable struct {
	RouteTableId string   `json:"RouteTableId"`
	VpcId        string   `json:"VpcId"`
	Name         string   `json:"Name"`
	Routes       []Route  `json:"Routes"`
	SubnetIds    []string `json:"SubnetIds"`
	IsMain       bool     `json:"IsMain"`
}

type Route struct {
	Destination  string `json:"DestinationCidrBlock"`
	GatewayId    string `json:"GatewayId"`
	NatGatewayId string `json:"NatGatewayId"`
	State        string `json:"State"`
}

type SecurityGroup struct {
	GroupId     string   `json:"GroupId"`
	GroupName   string   `json:"GroupName"`
	Description string   `json:"Description"`
	VpcId       string   `json:"VpcId"`
	InboundCount  int    `json:"InboundCount"`
	OutboundCount int    `json:"OutboundCount"`
	Name        string   `json:"Name"`
}

func LoadVPCData(region string) (*VPCData, error) {
	data := &VPCData{}

	if raw, err := ReadCache(region + ":vpcs"); err == nil && raw != nil {
		var resp struct{ Vpcs []json.RawMessage }
		json.Unmarshal(raw, &resp)
		for _, v := range resp.Vpcs {
			data.VPCs = append(data.VPCs, parseVPC(v))
		}
	}

	if raw, err := ReadCache(region + ":subnets"); err == nil && raw != nil {
		var resp struct{ Subnets []json.RawMessage }
		json.Unmarshal(raw, &resp)
		for _, s := range resp.Subnets {
			data.Subnets = append(data.Subnets, parseSubnet(s))
		}
	}

	if raw, err := ReadCache(region + ":igws"); err == nil && raw != nil {
		var resp struct{ InternetGateways []json.RawMessage }
		json.Unmarshal(raw, &resp)
		for _, g := range resp.InternetGateways {
			data.IGWs = append(data.IGWs, parseIGW(g))
		}
	}

	if raw, err := ReadCache(region + ":nat-gws"); err == nil && raw != nil {
		var resp struct{ NatGateways []json.RawMessage }
		json.Unmarshal(raw, &resp)
		for _, n := range resp.NatGateways {
			data.NATGWs = append(data.NATGWs, parseNATGW(n))
		}
	}

	if raw, err := ReadCache(region + ":route-tables"); err == nil && raw != nil {
		var resp struct{ RouteTables []json.RawMessage }
		json.Unmarshal(raw, &resp)
		for _, r := range resp.RouteTables {
			data.RouteTables = append(data.RouteTables, parseRouteTable(r))
		}
	}

	if raw, err := ReadCache(region + ":security-groups"); err == nil && raw != nil {
		var resp struct{ SecurityGroups []json.RawMessage }
		json.Unmarshal(raw, &resp)
		for _, s := range resp.SecurityGroups {
			data.SecurityGroups = append(data.SecurityGroups, parseSG(s))
		}
	}

	return data, nil
}

func tagName(raw json.RawMessage) string {
	var obj struct {
		Tags []struct {
			Key   string `json:"Key"`
			Value string `json:"Value"`
		} `json:"Tags"`
	}
	json.Unmarshal(raw, &obj)
	for _, t := range obj.Tags {
		if t.Key == "Name" {
			return t.Value
		}
	}
	return ""
}

func parseVPC(raw json.RawMessage) VPC {
	var v VPC
	json.Unmarshal(raw, &v)
	v.Name = tagName(raw)
	return v
}

func parseSubnet(raw json.RawMessage) Subnet {
	var s Subnet
	json.Unmarshal(raw, &s)
	s.Name = tagName(raw)
	return s
}

func parseIGW(raw json.RawMessage) IGW {
	var g struct {
		InternetGatewayId string `json:"InternetGatewayId"`
		Attachments       []struct {
			VpcId string `json:"VpcId"`
		} `json:"Attachments"`
	}
	json.Unmarshal(raw, &g)
	igw := IGW{
		InternetGatewayId: g.InternetGatewayId,
		Name:              tagName(raw),
	}
	for _, a := range g.Attachments {
		igw.AttachedVpcIds = append(igw.AttachedVpcIds, a.VpcId)
	}
	return igw
}

func parseNATGW(raw json.RawMessage) NATGW {
	var n NATGW
	json.Unmarshal(raw, &n)
	n.Name = tagName(raw)
	return n
}

func parseRouteTable(raw json.RawMessage) RouteTable {
	var rt struct {
		RouteTableId string  `json:"RouteTableId"`
		VpcId        string  `json:"VpcId"`
		Routes       []Route `json:"Routes"`
		Associations []struct {
			Main     bool   `json:"Main"`
			SubnetId string `json:"SubnetId"`
		} `json:"Associations"`
	}
	json.Unmarshal(raw, &rt)
	result := RouteTable{
		RouteTableId: rt.RouteTableId,
		VpcId:        rt.VpcId,
		Name:         tagName(raw),
		Routes:       rt.Routes,
	}
	for _, a := range rt.Associations {
		if a.Main {
			result.IsMain = true
		}
		if a.SubnetId != "" {
			result.SubnetIds = append(result.SubnetIds, a.SubnetId)
		}
	}
	return result
}

func parseSG(raw json.RawMessage) SecurityGroup {
	var sg struct {
		GroupId          string        `json:"GroupId"`
		GroupName        string        `json:"GroupName"`
		Description      string        `json:"Description"`
		VpcId            string        `json:"VpcId"`
		IpPermissions    []interface{} `json:"IpPermissions"`
		IpPermissionsEgress []interface{} `json:"IpPermissionsEgress"`
	}
	json.Unmarshal(raw, &sg)
	return SecurityGroup{
		GroupId:       sg.GroupId,
		GroupName:     sg.GroupName,
		Description:   sg.Description,
		VpcId:         sg.VpcId,
		InboundCount:  len(sg.IpPermissions),
		OutboundCount: len(sg.IpPermissionsEgress),
		Name:          tagName(raw),
	}
}
