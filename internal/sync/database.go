package sync

import (
	"encoding/json"

	"github.com/estrados/simply-aws/internal/awscli"
)

type DatabaseData struct {
	RDS         []RDSInstance    `json:"rds"`
	DynamoDB    []DynamoDBTable `json:"dynamodb"`
	ElastiCache []ElastiCacheCluster `json:"elasticache"`
}

type RDSInstance struct {
	DBInstanceId       string   `json:"DBInstanceIdentifier"`
	Engine             string   `json:"Engine"`
	EngineVersion      string   `json:"EngineVersion"`
	InstanceClass      string   `json:"DBInstanceClass"`
	Status             string   `json:"DBInstanceStatus"`
	MultiAZ            bool     `json:"MultiAZ"`
	StorageType        string   `json:"StorageType"`
	AllocatedStorage   int      `json:"AllocatedStorage"`
	Endpoint           string   `json:"Endpoint"`
	Port               int      `json:"Port"`
	VpcId              string   `json:"VpcId"`
	SubnetGroupName    string   `json:"SubnetGroupName"`
	PubliclyAccessible bool     `json:"PubliclyAccessible"`
	SecurityGroups     []string `json:"SecurityGroups"`
}

type DynamoDBTable struct {
	TableName    string `json:"TableName"`
	Status       string `json:"TableStatus"`
	ItemCount    int64  `json:"ItemCount"`
	SizeBytes    int64  `json:"TableSizeBytes"`
	BillingMode  string `json:"BillingMode"`
	TableClass   string `json:"TableClass"`
}

type ElastiCacheCluster struct {
	CacheClusterId   string   `json:"CacheClusterId"`
	Engine           string   `json:"Engine"`
	EngineVersion    string   `json:"EngineVersion"`
	CacheNodeType    string   `json:"CacheNodeType"`
	NumNodes         int      `json:"NumCacheNodes"`
	Status           string   `json:"CacheClusterStatus"`
	Endpoint         string   `json:"Endpoint"`
	Port             int      `json:"Port"`
	SubnetGroupName  string   `json:"SubnetGroupName"`
	VpcId            string   `json:"VpcId"`
	SecurityGroups   []string `json:"SecurityGroups"`
}

func SyncDatabaseData(region string) ([]SyncResult, error) {
	var results []SyncResult

	// Sync security groups so SG detail links work from this tab
	if data, err := awscli.Run("ec2", "describe-security-groups", "--region", region); err == nil {
		WriteCache(region+":security-groups", data)
	}

	// RDS
	if data, err := awscli.Run("rds", "describe-db-instances", "--region", region); err == nil {
		WriteCache(region+":rds", data)
		results = append(results, SyncResult{Service: "rds", Count: countKey(data, "DBInstances")})
	} else {
		results = append(results, SyncResult{Service: "rds", Error: err.Error()})
	}

	// DynamoDB - list then describe each
	if data, err := awscli.Run("dynamodb", "list-tables", "--region", region); err == nil {
		var resp struct {
			TableNames []string `json:"TableNames"`
		}
		json.Unmarshal(data, &resp)

		var tables []DynamoDBTable
		for _, name := range resp.TableNames {
			if tData, err := awscli.Run("dynamodb", "describe-table", "--table-name", name, "--region", region); err == nil {
				tables = append(tables, parseDynamoDBTable(tData))
			}
		}
		tablesJSON, _ := json.Marshal(tables)
		WriteCache(region+":dynamodb", tablesJSON)
		results = append(results, SyncResult{Service: "dynamodb", Count: len(tables)})
	} else {
		results = append(results, SyncResult{Service: "dynamodb", Error: err.Error()})
	}

	// ElastiCache - fetch and enrich with VPC info
	if data, err := awscli.Run("elasticache", "describe-cache-clusters", "--show-cache-node-info", "--region", region); err == nil {
		var resp struct {
			CacheClusters []json.RawMessage `json:"CacheClusters"`
		}
		json.Unmarshal(data, &resp)
		var clusters []ElastiCacheCluster
		for _, c := range resp.CacheClusters {
			clusters = append(clusters, parseElastiCache(c, region))
		}
		enriched, _ := json.Marshal(clusters)
		WriteCache(region+":elasticache-enriched", enriched)
		results = append(results, SyncResult{Service: "elasticache", Count: len(clusters)})
	} else {
		results = append(results, SyncResult{Service: "elasticache", Error: err.Error()})
	}

	return results, nil
}

func LoadDatabaseData(region string) (*DatabaseData, error) {
	data := &DatabaseData{}

	// RDS
	if raw, err := ReadCache(region + ":rds"); err == nil && raw != nil {
		var resp struct {
			DBInstances []json.RawMessage `json:"DBInstances"`
		}
		json.Unmarshal(raw, &resp)
		for _, r := range resp.DBInstances {
			data.RDS = append(data.RDS, parseRDSInstance(r))
		}
	}

	// DynamoDB
	if raw, err := ReadCache(region + ":dynamodb"); err == nil && raw != nil {
		json.Unmarshal(raw, &data.DynamoDB)
	}

	// ElastiCache (enriched during sync)
	if raw, err := ReadCache(region + ":elasticache-enriched"); err == nil && raw != nil {
		json.Unmarshal(raw, &data.ElastiCache)
	}

	return data, nil
}

func parseRDSInstance(raw json.RawMessage) RDSInstance {
	var r struct {
		DBInstanceIdentifier string `json:"DBInstanceIdentifier"`
		Engine               string `json:"Engine"`
		EngineVersion        string `json:"EngineVersion"`
		DBInstanceClass      string `json:"DBInstanceClass"`
		DBInstanceStatus     string `json:"DBInstanceStatus"`
		MultiAZ              bool   `json:"MultiAZ"`
		StorageType          string `json:"StorageType"`
		AllocatedStorage     int    `json:"AllocatedStorage"`
		PubliclyAccessible   bool   `json:"PubliclyAccessible"`
		Endpoint             *struct {
			Address string `json:"Address"`
			Port    int    `json:"Port"`
		} `json:"Endpoint"`
		DBSubnetGroup *struct {
			DBSubnetGroupName string `json:"DBSubnetGroupName"`
			VpcId             string `json:"VpcId"`
		} `json:"DBSubnetGroup"`
		VpcSecurityGroups []struct {
			VpcSecurityGroupId string `json:"VpcSecurityGroupId"`
		} `json:"VpcSecurityGroups"`
	}
	json.Unmarshal(raw, &r)

	inst := RDSInstance{
		DBInstanceId:       r.DBInstanceIdentifier,
		Engine:             r.Engine,
		EngineVersion:      r.EngineVersion,
		InstanceClass:      r.DBInstanceClass,
		Status:             r.DBInstanceStatus,
		MultiAZ:            r.MultiAZ,
		StorageType:        r.StorageType,
		AllocatedStorage:   r.AllocatedStorage,
		PubliclyAccessible: r.PubliclyAccessible,
	}
	if r.Endpoint != nil {
		inst.Endpoint = r.Endpoint.Address
		inst.Port = r.Endpoint.Port
	}
	if r.DBSubnetGroup != nil {
		inst.VpcId = r.DBSubnetGroup.VpcId
		inst.SubnetGroupName = r.DBSubnetGroup.DBSubnetGroupName
	}
	for _, sg := range r.VpcSecurityGroups {
		inst.SecurityGroups = append(inst.SecurityGroups, sg.VpcSecurityGroupId)
	}
	return inst
}

func parseDynamoDBTable(raw json.RawMessage) DynamoDBTable {
	var resp struct {
		Table struct {
			TableName      string `json:"TableName"`
			TableStatus    string `json:"TableStatus"`
			ItemCount      int64  `json:"ItemCount"`
			TableSizeBytes int64  `json:"TableSizeBytes"`
			BillingModeSummary *struct {
				BillingMode string `json:"BillingMode"`
			} `json:"BillingModeSummary"`
			TableClassSummary *struct {
				TableClass string `json:"TableClass"`
			} `json:"TableClassSummary"`
		} `json:"Table"`
	}
	json.Unmarshal(raw, &resp)
	t := resp.Table

	billing := "PROVISIONED"
	if t.BillingModeSummary != nil && t.BillingModeSummary.BillingMode != "" {
		billing = t.BillingModeSummary.BillingMode
	}
	class := "STANDARD"
	if t.TableClassSummary != nil && t.TableClassSummary.TableClass != "" {
		class = t.TableClassSummary.TableClass
	}

	return DynamoDBTable{
		TableName:   t.TableName,
		Status:      t.TableStatus,
		ItemCount:   t.ItemCount,
		SizeBytes:   t.TableSizeBytes,
		BillingMode: billing,
		TableClass:  class,
	}
}

func parseElastiCache(raw json.RawMessage, region string) ElastiCacheCluster {
	var r struct {
		CacheClusterId       string `json:"CacheClusterId"`
		Engine               string `json:"Engine"`
		EngineVersion        string `json:"EngineVersion"`
		CacheNodeType        string `json:"CacheNodeType"`
		NumCacheNodes        int    `json:"NumCacheNodes"`
		CacheClusterStatus   string `json:"CacheClusterStatus"`
		CacheSubnetGroupName string `json:"CacheSubnetGroupName"`
		ConfigurationEndpoint *struct {
			Address string `json:"Address"`
			Port    int    `json:"Port"`
		} `json:"ConfigurationEndpoint"`
		CacheNodes []struct {
			Endpoint *struct {
				Address string `json:"Address"`
				Port    int    `json:"Port"`
			} `json:"Endpoint"`
		} `json:"CacheNodes"`
		SecurityGroups []struct {
			SecurityGroupId string `json:"SecurityGroupId"`
		} `json:"SecurityGroups"`
	}
	json.Unmarshal(raw, &r)
	c := ElastiCacheCluster{
		CacheClusterId:  r.CacheClusterId,
		Engine:          r.Engine,
		EngineVersion:   r.EngineVersion,
		CacheNodeType:   r.CacheNodeType,
		NumNodes:        r.NumCacheNodes,
		Status:          r.CacheClusterStatus,
		SubnetGroupName: r.CacheSubnetGroupName,
	}
	if r.ConfigurationEndpoint != nil {
		c.Endpoint = r.ConfigurationEndpoint.Address
		c.Port = r.ConfigurationEndpoint.Port
	} else if len(r.CacheNodes) > 0 && r.CacheNodes[0].Endpoint != nil {
		c.Endpoint = r.CacheNodes[0].Endpoint.Address
		c.Port = r.CacheNodes[0].Endpoint.Port
	}
	// Look up VPC from subnet group
	if r.CacheSubnetGroupName != "" {
		if sgData, err := awscli.Run("elasticache", "describe-cache-subnet-groups",
			"--cache-subnet-group-name", r.CacheSubnetGroupName, "--region", region); err == nil {
			var sgResp struct {
				CacheSubnetGroups []struct {
					VpcId string `json:"VpcId"`
				} `json:"CacheSubnetGroups"`
			}
			json.Unmarshal(sgData, &sgResp)
			if len(sgResp.CacheSubnetGroups) > 0 {
				c.VpcId = sgResp.CacheSubnetGroups[0].VpcId
			}
		}
	}
	for _, sg := range r.SecurityGroups {
		c.SecurityGroups = append(c.SecurityGroups, sg.SecurityGroupId)
	}
	return c
}
