package sync

import (
	"encoding/json"

	"github.com/estrados/simply-aws/internal/awscli"
)

type SyncResult struct {
	Service string `json:"service"`
	Count   int    `json:"count"`
	Error   string `json:"error,omitempty"`
}

// SyncVPCData fetches all VPC-related resources for a region and caches them.
func SyncVPCData(region string) ([]SyncResult, error) {
	jobs := []struct {
		name      string
		args      []string
		countKey  string
	}{
		{"vpcs", []string{"ec2", "describe-vpcs", "--region", region}, "Vpcs"},
		{"subnets", []string{"ec2", "describe-subnets", "--region", region}, "Subnets"},
		{"igws", []string{"ec2", "describe-internet-gateways", "--region", region}, "InternetGateways"},
		{"nat-gws", []string{"ec2", "describe-nat-gateways", "--region", region}, "NatGateways"},
		{"route-tables", []string{"ec2", "describe-route-tables", "--region", region}, "RouteTables"},
		{"security-groups", []string{"ec2", "describe-security-groups", "--region", region}, "SecurityGroups"},
	}

	var results []SyncResult
	for _, job := range jobs {
		key := region + ":" + job.name
		data, err := awscli.Run(job.args...)
		if err != nil {
			results = append(results, SyncResult{Service: job.name, Error: err.Error()})
			continue
		}
		WriteCache(key, data)
		results = append(results, SyncResult{Service: job.name, Count: countKey(data, job.countKey)})
	}

	return results, nil
}

// SyncAll fetches common resources (not region-specific like S3).
func SyncAll() ([]SyncResult, error) {
	jobs := []struct {
		name string
		fn   func() (*SyncResult, error)
	}{
		{"ec2", syncEC2},
		{"ecs", syncECS},
		{"rds", syncRDS},
		{"s3", syncS3},
		{"cloudformation", syncCFStacks},
	}

	var results []SyncResult
	var synced []string

	for _, job := range jobs {
		result, err := job.fn()
		if err != nil {
			results = append(results, SyncResult{Service: job.name, Error: err.Error()})
			continue
		}
		results = append(results, *result)
		synced = append(synced, job.name)
	}

	WriteLastSync(synced)
	return results, nil
}

func syncService(name string, args []string, countField string) (*SyncResult, error) {
	data, err := awscli.Run(args...)
	if err != nil {
		return nil, err
	}
	if err := WriteCache(name, data); err != nil {
		return nil, err
	}
	return &SyncResult{Service: name, Count: countKey(data, countField)}, nil
}

func syncEC2() (*SyncResult, error) {
	return syncService("ec2", []string{"ec2", "describe-instances"}, "Reservations")
}

func syncECS() (*SyncResult, error) {
	return syncService("ecs", []string{"ecs", "list-clusters"}, "clusterArns")
}

func syncRDS() (*SyncResult, error) {
	return syncService("rds", []string{"rds", "describe-db-instances"}, "DBInstances")
}

func syncS3() (*SyncResult, error) {
	return syncService("s3", []string{"s3api", "list-buckets"}, "Buckets")
}

func syncCFStacks() (*SyncResult, error) {
	return syncService("cloudformation", []string{"cloudformation", "describe-stacks"}, "Stacks")
}

func countKey(data json.RawMessage, key string) int {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return 0
	}
	val, ok := m[key]
	if !ok {
		return 0
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(val, &arr); err != nil {
		return 0
	}
	return len(arr)
}
