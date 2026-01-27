package sync

import (
	"encoding/json"
	"strings"

	"github.com/estrados/simply-aws/internal/awscli"
)

type ComputeData struct {
	EC2    []EC2Instance    `json:"ec2"`
	ECS    []ECSCluster     `json:"ecs"`
	Lambda []LambdaFunction `json:"lambda"`
}

type EC2Instance struct {
	InstanceId     string   `json:"InstanceId"`
	Name           string   `json:"Name"`
	InstanceType   string   `json:"InstanceType"`
	State          string   `json:"State"`
	PublicIP       string   `json:"PublicIP"`
	PrivateIP      string   `json:"PrivateIP"`
	VpcId          string   `json:"VpcId"`
	SubnetId       string   `json:"SubnetId"`
	SecurityGroups []string `json:"SecurityGroups"`
	LaunchTime     string   `json:"LaunchTime"`
	IamRole        string   `json:"IamRole"`
	IamPolicies    []string `json:"IamPolicies"`
}

type ECSCluster struct {
	ClusterName       string   `json:"ClusterName"`
	ClusterArn        string   `json:"ClusterArn"`
	Status            string   `json:"Status"`
	RunningTasks      int      `json:"RunningTasks"`
	PendingTasks      int      `json:"PendingTasks"`
	Services          int      `json:"Services"`
	CapacityProviders []string `json:"CapacityProviders"`
}

type LambdaFunction struct {
	FunctionName   string   `json:"FunctionName"`
	Runtime        string   `json:"Runtime"`
	Handler        string   `json:"Handler"`
	State          string   `json:"State"`
	MemorySize     int      `json:"MemorySize"`
	Timeout        int      `json:"Timeout"`
	CodeSize       int64    `json:"CodeSize"`
	LastModified   string   `json:"LastModified"`
	FunctionUrl    string           `json:"FunctionUrl"`
	Policies       []ResourcePolicy `json:"Policies"`
	VpcId          string           `json:"VpcId"`
	SubnetIds      []string         `json:"SubnetIds"`
	SecurityGroups []string         `json:"SecurityGroups"`
	IamRole        string           `json:"IamRole"`
	IamPolicies    []string         `json:"IamPolicies"`
}

func SyncComputeData(region string) ([]SyncResult, error) {
	var results []SyncResult

	// Sync security groups so SG detail links work from this tab
	if data, err := awscli.Run("ec2", "describe-security-groups", "--region", region); err == nil {
		WriteCache(region+":security-groups", data)
	}

	// EC2
	if data, err := awscli.Run("ec2", "describe-instances", "--region", region); err == nil {
		WriteCache(region+":ec2", data)
		var resp struct {
			Reservations []struct {
				Instances []json.RawMessage `json:"Instances"`
			} `json:"Reservations"`
		}
		json.Unmarshal(data, &resp)
		var instances []EC2Instance
		for _, r := range resp.Reservations {
			for _, inst := range r.Instances {
				instances = append(instances, parseEC2Instance(inst))
			}
		}
		enriched, _ := json.Marshal(instances)
		WriteCache(region+":ec2-enriched", enriched)
		results = append(results, SyncResult{Service: "ec2", Count: len(instances)})
	} else {
		results = append(results, SyncResult{Service: "ec2", Error: err.Error()})
	}

	// ECS - list clusters, then describe
	if data, err := awscli.Run("ecs", "list-clusters", "--region", region); err == nil {
		var resp struct {
			ClusterArns []string `json:"clusterArns"`
		}
		json.Unmarshal(data, &resp)

		var clusters []ECSCluster
		if len(resp.ClusterArns) > 0 {
			args := []string{"describe-clusters", "--region", region, "--include", "SETTINGS", "--clusters"}
			args = append(args, resp.ClusterArns...)
			if descData, err := awscli.Run(append([]string{"ecs"}, args...)...); err == nil {
				var descResp struct {
					Clusters []json.RawMessage `json:"clusters"`
				}
				json.Unmarshal(descData, &descResp)
				for _, c := range descResp.Clusters {
					clusters = append(clusters, parseECSCluster(c))
				}
			}
		}
		enriched, _ := json.Marshal(clusters)
		WriteCache(region+":ecs-enriched", enriched)
		results = append(results, SyncResult{Service: "ecs", Count: len(clusters)})
	} else {
		results = append(results, SyncResult{Service: "ecs", Error: err.Error()})
	}

	// Lambda
	if data, err := awscli.Run("lambda", "list-functions", "--region", region); err == nil {
		var resp struct {
			Functions []json.RawMessage `json:"Functions"`
		}
		json.Unmarshal(data, &resp)
		var functions []LambdaFunction
		for _, f := range resp.Functions {
			fn := parseLambdaFunction(f)
			// Check for Function URL
			if urlData, err := awscli.Run("lambda", "get-function-url-config",
				"--function-name", fn.FunctionName, "--region", region); err == nil {
				var urlResp struct {
					FunctionUrl string `json:"FunctionUrl"`
				}
				json.Unmarshal(urlData, &urlResp)
				fn.FunctionUrl = urlResp.FunctionUrl
			}
			// Fetch resource policy
			if polData, err := awscli.Run("lambda", "get-policy",
				"--function-name", fn.FunctionName, "--region", region); err == nil {
				var polResp struct {
					Policy string `json:"Policy"`
				}
				json.Unmarshal(polData, &polResp)
				fn.Policies = ParseResourcePolicies(polResp.Policy)
			}
			functions = append(functions, fn)
		}
		enriched, _ := json.Marshal(functions)
		WriteCache(region+":lambda", enriched)
		results = append(results, SyncResult{Service: "lambda", Count: len(functions)})
	} else {
		results = append(results, SyncResult{Service: "lambda", Error: err.Error()})
	}

	return results, nil
}

func LoadComputeData(region string) (*ComputeData, error) {
	data := &ComputeData{}

	// EC2 (enriched with IAM role/policies during sync)
	if raw, err := ReadCache(region + ":ec2-enriched"); err == nil && raw != nil {
		json.Unmarshal(raw, &data.EC2)
	} else if raw, err := ReadCache(region + ":ec2"); err == nil && raw != nil {
		// Fallback to raw cache if not yet enriched
		var resp struct {
			Reservations []struct {
				Instances []json.RawMessage `json:"Instances"`
			} `json:"Reservations"`
		}
		json.Unmarshal(raw, &resp)
		for _, r := range resp.Reservations {
			for _, inst := range r.Instances {
				data.EC2 = append(data.EC2, parseEC2Instance(inst))
			}
		}
	}

	// ECS (enriched during sync)
	if raw, err := ReadCache(region + ":ecs-enriched"); err == nil && raw != nil {
		json.Unmarshal(raw, &data.ECS)
	}

	// Lambda
	if raw, err := ReadCache(region + ":lambda"); err == nil && raw != nil {
		json.Unmarshal(raw, &data.Lambda)
	}

	return data, nil
}

func parseEC2Instance(raw json.RawMessage) EC2Instance {
	var r struct {
		InstanceId   string `json:"InstanceId"`
		InstanceType string `json:"InstanceType"`
		State        struct {
			Name string `json:"Name"`
		} `json:"State"`
		PublicIpAddress  string `json:"PublicIpAddress"`
		PrivateIpAddress string `json:"PrivateIpAddress"`
		VpcId            string `json:"VpcId"`
		SubnetId         string `json:"SubnetId"`
		LaunchTime       string `json:"LaunchTime"`
		Tags             []struct {
			Key   string `json:"Key"`
			Value string `json:"Value"`
		} `json:"Tags"`
		SecurityGroups []struct {
			GroupId string `json:"GroupId"`
		} `json:"SecurityGroups"`
		IamInstanceProfile *struct {
			Arn string `json:"Arn"`
		} `json:"IamInstanceProfile"`
	}
	json.Unmarshal(raw, &r)

	inst := EC2Instance{
		InstanceId:   r.InstanceId,
		InstanceType: r.InstanceType,
		State:        r.State.Name,
		PublicIP:     r.PublicIpAddress,
		PrivateIP:    r.PrivateIpAddress,
		VpcId:        r.VpcId,
		SubnetId:     r.SubnetId,
		LaunchTime:   r.LaunchTime,
	}
	for _, tag := range r.Tags {
		if tag.Key == "Name" {
			inst.Name = tag.Value
			break
		}
	}
	for _, sg := range r.SecurityGroups {
		inst.SecurityGroups = append(inst.SecurityGroups, sg.GroupId)
	}
	// Resolve IAM instance profile → role → policies
	if r.IamInstanceProfile != nil && r.IamInstanceProfile.Arn != "" {
		inst.IamRole, inst.IamPolicies = resolveInstanceProfile(r.IamInstanceProfile.Arn)
	}
	return inst
}

func resolveInstanceProfile(profileArn string) (roleName string, policies []string) {
	// Extract instance profile name from ARN
	// arn:aws:iam::123456:instance-profile/MyProfile
	parts := strings.Split(profileArn, "/")
	profileName := parts[len(parts)-1]

	// Get instance profile to find the role
	if data, err := awscli.Run("iam", "get-instance-profile",
		"--instance-profile-name", profileName); err == nil {
		var resp struct {
			InstanceProfile struct {
				Roles []struct {
					RoleName string `json:"RoleName"`
				} `json:"Roles"`
			} `json:"InstanceProfile"`
		}
		json.Unmarshal(data, &resp)
		if len(resp.InstanceProfile.Roles) > 0 {
			roleName = resp.InstanceProfile.Roles[0].RoleName

			// Get attached policies for this role
			if polData, err := awscli.Run("iam", "list-attached-role-policies",
				"--role-name", roleName); err == nil {
				var polResp struct {
					AttachedPolicies []struct {
						PolicyName string `json:"PolicyName"`
					} `json:"AttachedPolicies"`
				}
				json.Unmarshal(polData, &polResp)
				for _, p := range polResp.AttachedPolicies {
					policies = append(policies, p.PolicyName)
				}
			}

			// Also get inline policies
			if polData, err := awscli.Run("iam", "list-role-policies",
				"--role-name", roleName); err == nil {
				var polResp struct {
					PolicyNames []string `json:"PolicyNames"`
				}
				json.Unmarshal(polData, &polResp)
				for _, p := range polResp.PolicyNames {
					policies = append(policies, p+" (inline)")
				}
			}
		}
	}
	return
}

func parseECSCluster(raw json.RawMessage) ECSCluster {
	var r struct {
		ClusterName              string   `json:"clusterName"`
		ClusterArn               string   `json:"clusterArn"`
		Status                   string   `json:"status"`
		RunningTasksCount        int      `json:"runningTasksCount"`
		PendingTasksCount        int      `json:"pendingTasksCount"`
		ActiveServicesCount      int      `json:"activeServicesCount"`
		CapacityProviders        []string `json:"capacityProviders"`
	}
	json.Unmarshal(raw, &r)

	return ECSCluster{
		ClusterName:       r.ClusterName,
		ClusterArn:        r.ClusterArn,
		Status:            r.Status,
		RunningTasks:      r.RunningTasksCount,
		PendingTasks:      r.PendingTasksCount,
		Services:          r.ActiveServicesCount,
		CapacityProviders: r.CapacityProviders,
	}
}

func parseLambdaFunction(raw json.RawMessage) LambdaFunction {
	var r struct {
		FunctionName string `json:"FunctionName"`
		Runtime      string `json:"Runtime"`
		Handler      string `json:"Handler"`
		State        string `json:"State"`
		MemorySize   int    `json:"MemorySize"`
		Timeout      int    `json:"Timeout"`
		CodeSize     int64  `json:"CodeSize"`
		LastModified string `json:"LastModified"`
		Role         string `json:"Role"`
		VpcConfig    *struct {
			VpcId            string   `json:"VpcId"`
			SubnetIds        []string `json:"SubnetIds"`
			SecurityGroupIds []string `json:"SecurityGroupIds"`
		} `json:"VpcConfig"`
	}
	json.Unmarshal(raw, &r)

	fn := LambdaFunction{
		FunctionName: r.FunctionName,
		Runtime:      r.Runtime,
		Handler:      r.Handler,
		State:        r.State,
		MemorySize:   r.MemorySize,
		Timeout:      r.Timeout,
		CodeSize:     r.CodeSize,
		LastModified: r.LastModified,
	}
	if r.VpcConfig != nil && r.VpcConfig.VpcId != "" {
		fn.VpcId = r.VpcConfig.VpcId
		fn.SubnetIds = r.VpcConfig.SubnetIds
		fn.SecurityGroups = r.VpcConfig.SecurityGroupIds
	}
	// Resolve IAM execution role → policies
	if r.Role != "" {
		parts := strings.Split(r.Role, "/")
		roleName := parts[len(parts)-1]
		fn.IamRole = roleName
		if polData, err := awscli.Run("iam", "list-attached-role-policies",
			"--role-name", roleName); err == nil {
			var polResp struct {
				AttachedPolicies []struct {
					PolicyName string `json:"PolicyName"`
				} `json:"AttachedPolicies"`
			}
			json.Unmarshal(polData, &polResp)
			for _, p := range polResp.AttachedPolicies {
				fn.IamPolicies = append(fn.IamPolicies, p.PolicyName)
			}
		}
		if polData, err := awscli.Run("iam", "list-role-policies",
			"--role-name", roleName); err == nil {
			var polResp struct {
				PolicyNames []string `json:"PolicyNames"`
			}
			json.Unmarshal(polData, &polResp)
			for _, p := range polResp.PolicyNames {
				fn.IamPolicies = append(fn.IamPolicies, p+" (inline)")
			}
		}
	}
	return fn
}

