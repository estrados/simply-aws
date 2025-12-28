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
	DBInstanceId    string `json:"DBInstanceIdentifier"`
	Engine          string `json:"Engine"`
	EngineVersion   string `json:"EngineVersion"`
	InstanceClass   string `json:"DBInstanceClass"`
	Status          string `json:"DBInstanceStatus"`
	MultiAZ         bool   `json:"MultiAZ"`
	StorageType     string `json:"StorageType"`
	AllocatedStorage int   `json:"AllocatedStorage"`
	Endpoint        string `json:"Endpoint"`
	Port            int    `json:"Port"`
	VpcId           string `json:"VpcId"`
	PubliclyAccessible bool `json:"PubliclyAccessible"`
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
	CacheClusterId string `json:"CacheClusterId"`
	Engine         string `json:"Engine"`
	EngineVersion  string `json:"EngineVersion"`
	CacheNodeType  string `json:"CacheNodeType"`
	NumNodes       int    `json:"NumCacheNodes"`
	Status         string `json:"CacheClusterStatus"`
}

func SyncDatabaseData(region string) ([]SyncResult, error) {
	var results []SyncResult

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

	// ElastiCache
	if data, err := awscli.Run("elasticache", "describe-cache-clusters", "--region", region); err == nil {
		WriteCache(region+":elasticache", data)
		results = append(results, SyncResult{Service: "elasticache", Count: countKey(data, "CacheClusters")})
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

	// ElastiCache
	if raw, err := ReadCache(region + ":elasticache"); err == nil && raw != nil {
		var resp struct {
			CacheClusters []json.RawMessage `json:"CacheClusters"`
		}
		json.Unmarshal(raw, &resp)
		for _, c := range resp.CacheClusters {
			data.ElastiCache = append(data.ElastiCache, parseElastiCache(c))
		}
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
			VpcId string `json:"VpcId"`
		} `json:"DBSubnetGroup"`
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

func parseElastiCache(raw json.RawMessage) ElastiCacheCluster {
	var c ElastiCacheCluster
	json.Unmarshal(raw, &c)
	return c
}
