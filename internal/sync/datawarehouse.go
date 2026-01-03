package sync

import (
	"encoding/json"
	"time"

	"github.com/estrados/simply-aws/internal/awscli"
)

type DataWarehouseData struct {
	Redshift []RedshiftCluster `json:"redshift"`
	Athena   []AthenaWorkgroup `json:"athena"`
	Glue     []GlueDatabase    `json:"glue"`
}

type RedshiftCluster struct {
	ClusterIdentifier  string              `json:"ClusterIdentifier"`
	NodeType           string              `json:"NodeType"`
	NumberOfNodes      int                 `json:"NumberOfNodes"`
	Status             string              `json:"ClusterStatus"`
	DBName             string              `json:"DBName"`
	Endpoint           string              `json:"Endpoint"`
	Port               int                 `json:"Port"`
	VpcId              string              `json:"VpcId"`
	SubnetGroupName    string              `json:"SubnetGroupName"`
	Encrypted          bool                `json:"Encrypted"`
	PubliclyAccessible bool                `json:"PubliclyAccessible"`
	SecurityGroups     []RedshiftSG        `json:"SecurityGroups"`
}

type RedshiftSG struct {
	GroupId string `json:"VpcSecurityGroupId"`
	Status  string `json:"Status"`
}

type AthenaWorkgroup struct {
	Name          string `json:"Name"`
	State         string `json:"State"`
	Description   string `json:"Description"`
	EngineVersion string `json:"EngineVersion"`
	CreationTime  string `json:"CreationTime"`
}

type GlueDatabase struct {
	Name         string `json:"Name"`
	Description  string `json:"Description"`
	LocationUri  string `json:"LocationUri"`
	CreateTime   string `json:"CreateTime"`
	CatalogId    string `json:"CatalogId"`
}

func SyncDataWarehouseData(region string) ([]SyncResult, error) {
	var results []SyncResult

	// Also sync security groups so SG detail links work from this tab
	if data, err := awscli.Run("ec2", "describe-security-groups", "--region", region); err == nil {
		WriteCache(region+":security-groups", data)
	}

	// Redshift
	if data, err := awscli.Run("redshift", "describe-clusters", "--region", region); err == nil {
		WriteCache(region+":redshift", data)
		results = append(results, SyncResult{Service: "redshift", Count: countKey(data, "Clusters")})
	} else {
		results = append(results, SyncResult{Service: "redshift", Error: err.Error()})
	}

	// Athena - list workgroups then get details
	if data, err := awscli.Run("athena", "list-work-groups", "--region", region); err == nil {
		var resp struct {
			WorkGroups []json.RawMessage `json:"WorkGroups"`
		}
		json.Unmarshal(data, &resp)

		var workgroups []AthenaWorkgroup
		for _, wg := range resp.WorkGroups {
			workgroups = append(workgroups, parseAthenaWorkgroup(wg))
		}
		wgJSON, _ := json.Marshal(workgroups)
		WriteCache(region+":athena", wgJSON)
		results = append(results, SyncResult{Service: "athena", Count: len(workgroups)})
	} else {
		results = append(results, SyncResult{Service: "athena", Error: err.Error()})
	}

	// Glue databases
	if data, err := awscli.Run("glue", "get-databases", "--region", region); err == nil {
		var resp struct {
			DatabaseList []json.RawMessage `json:"DatabaseList"`
		}
		json.Unmarshal(data, &resp)

		var databases []GlueDatabase
		for _, db := range resp.DatabaseList {
			databases = append(databases, parseGlueDatabase(db))
		}
		dbJSON, _ := json.Marshal(databases)
		WriteCache(region+":glue", dbJSON)
		results = append(results, SyncResult{Service: "glue", Count: len(databases)})
	} else {
		results = append(results, SyncResult{Service: "glue", Error: err.Error()})
	}

	return results, nil
}

func LoadDataWarehouseData(region string) (*DataWarehouseData, error) {
	data := &DataWarehouseData{}

	// Redshift
	if raw, err := ReadCache(region + ":redshift"); err == nil && raw != nil {
		var resp struct {
			Clusters []json.RawMessage `json:"Clusters"`
		}
		json.Unmarshal(raw, &resp)
		for _, c := range resp.Clusters {
			data.Redshift = append(data.Redshift, parseRedshiftCluster(c))
		}
	}

	// Athena
	if raw, err := ReadCache(region + ":athena"); err == nil && raw != nil {
		json.Unmarshal(raw, &data.Athena)
	}

	// Glue
	if raw, err := ReadCache(region + ":glue"); err == nil && raw != nil {
		json.Unmarshal(raw, &data.Glue)
	}

	return data, nil
}

func parseRedshiftCluster(raw json.RawMessage) RedshiftCluster {
	var r struct {
		ClusterIdentifier  string `json:"ClusterIdentifier"`
		NodeType           string `json:"NodeType"`
		NumberOfNodes      int    `json:"NumberOfNodes"`
		ClusterStatus      string `json:"ClusterStatus"`
		DBName             string `json:"DBName"`
		Encrypted          bool   `json:"Encrypted"`
		PubliclyAccessible bool   `json:"PubliclyAccessible"`
		Endpoint           *struct {
			Address string `json:"Address"`
			Port    int    `json:"Port"`
		} `json:"Endpoint"`
		VpcId                string `json:"VpcId"`
		ClusterSubnetGroupName string `json:"ClusterSubnetGroupName"`
		VpcSecurityGroups    []RedshiftSG `json:"VpcSecurityGroups"`
	}
	json.Unmarshal(raw, &r)

	c := RedshiftCluster{
		ClusterIdentifier:  r.ClusterIdentifier,
		NodeType:           r.NodeType,
		NumberOfNodes:      r.NumberOfNodes,
		Status:             r.ClusterStatus,
		DBName:             r.DBName,
		Encrypted:          r.Encrypted,
		PubliclyAccessible: r.PubliclyAccessible,
		VpcId:              r.VpcId,
		SubnetGroupName:    r.ClusterSubnetGroupName,
		SecurityGroups:     r.VpcSecurityGroups,
	}
	if r.Endpoint != nil {
		c.Endpoint = r.Endpoint.Address
		c.Port = r.Endpoint.Port
	}
	return c
}

func parseAthenaWorkgroup(raw json.RawMessage) AthenaWorkgroup {
	var wg struct {
		Name          string `json:"Name"`
		State         string `json:"State"`
		Description   string `json:"Description"`
		CreationTime  string `json:"CreationTime"`
		EngineVersion struct {
			EffectiveEngineVersion string `json:"EffectiveEngineVersion"`
		} `json:"EngineVersion"`
	}
	json.Unmarshal(raw, &wg)

	created := wg.CreationTime
	if t, err := time.Parse(time.RFC3339Nano, wg.CreationTime); err == nil {
		created = t.Format("2006-01-02 15:04")
	}

	return AthenaWorkgroup{
		Name:          wg.Name,
		State:         wg.State,
		Description:   wg.Description,
		EngineVersion: wg.EngineVersion.EffectiveEngineVersion,
		CreationTime:  created,
	}
}

func parseGlueDatabase(raw json.RawMessage) GlueDatabase {
	var db struct {
		Name        string `json:"Name"`
		Description string `json:"Description"`
		LocationUri string `json:"LocationUri"`
		CreateTime  string `json:"CreateTime"`
		CatalogId   string `json:"CatalogId"`
	}
	json.Unmarshal(raw, &db)

	created := db.CreateTime
	if t, err := time.Parse(time.RFC3339Nano, db.CreateTime); err == nil {
		created = t.Format("2006-01-02 15:04")
	}

	return GlueDatabase{
		Name:        db.Name,
		Description: db.Description,
		LocationUri: db.LocationUri,
		CreateTime:  created,
		CatalogId:   db.CatalogId,
	}
}
