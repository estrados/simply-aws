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
	InstanceId     string       `json:"InstanceId"`
	Name           string       `json:"Name"`
	InstanceType   string       `json:"InstanceType"`
	State          string       `json:"State"`
	PublicIP       string       `json:"PublicIP"`
	PrivateIP      string       `json:"PrivateIP"`
	VpcId          string       `json:"VpcId"`
	SubnetId       string       `json:"SubnetId"`
	SecurityGroups []string     `json:"SecurityGroups"`
	LaunchTime     string       `json:"LaunchTime"`
	IamRole        string       `json:"IamRole"`
	IamPolicies    []string     `json:"IamPolicies"`
	KeyName        string       `json:"KeyName"`
	ImageId        string       `json:"ImageId"`
	Volumes        []EC2Volume  `json:"Volumes"`
}

type EC2Volume struct {
	VolumeId   string `json:"VolumeId"`
	DeviceName string `json:"DeviceName"`
}

type ECSCluster struct {
	ClusterName       string            `json:"ClusterName"`
	ClusterArn        string            `json:"ClusterArn"`
	Status            string            `json:"Status"`
	RunningTasks      int               `json:"RunningTasks"`
	PendingTasks      int               `json:"PendingTasks"`
	Services          int               `json:"Services"`
	CapacityProviders []string          `json:"CapacityProviders"`
	TaskDefs          []ECSTaskDef      `json:"TaskDefs"`
	ECSServices       []ECSService      `json:"ECSServices"`
	Tasks             []ECSTask         `json:"Tasks"`
}

type ECSService struct {
	ServiceName    string   `json:"ServiceName"`
	Status         string   `json:"Status"`
	DesiredCount   int      `json:"DesiredCount"`
	RunningCount   int      `json:"RunningCount"`
	LaunchType     string   `json:"LaunchType"`
	TaskDefinition string   `json:"TaskDefinition"`
	SubnetIds      []string `json:"SubnetIds"`
	SecurityGroups []string `json:"SecurityGroups"`
	AssignPublicIP bool     `json:"AssignPublicIP"`
	LBTargetGroups []string `json:"LBTargetGroups"`
}

type ECSTask struct {
	TaskArn        string `json:"TaskArn"`
	TaskDefinition string `json:"TaskDefinition"`
	LastStatus     string `json:"LastStatus"`
	LaunchType     string `json:"LaunchType"`
	PrivateIP      string `json:"PrivateIP"`
	PublicIP       string `json:"PublicIP"`
	SubnetId       string `json:"SubnetId"`
}

type ECSTaskDef struct {
	Family            string   `json:"Family"`
	Revision          int      `json:"Revision"`
	TaskRoleName      string   `json:"TaskRoleName"`
	TaskRolePolicies  []string `json:"TaskRolePolicies"`
	ExecRoleName      string   `json:"ExecRoleName"`
	ExecRolePolicies  []string `json:"ExecRolePolicies"`
	LaunchType        string   `json:"LaunchType"`
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

func SyncComputeData(region string, onStep ...func(string)) ([]SyncResult, error) {
	step := func(label string) {
		if len(onStep) > 0 && onStep[0] != nil {
			onStep[0](label)
		}
	}
	var results []SyncResult

	// Sync security groups so SG detail links work from this tab
	if data, err := awscli.Run("ec2", "describe-security-groups", "--region", region); err == nil {
		WriteCache(region+":security-groups", data)
	}
	step("security groups")

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
	step("ec2")

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
		// Enrich with task definitions
		if tdData, err := awscli.Run("ecs", "list-task-definition-families",
			"--region", region, "--status", "ACTIVE"); err == nil {
			var tdResp struct {
				Families []string `json:"families"`
			}
			json.Unmarshal(tdData, &tdResp)
			var taskDefs []ECSTaskDef
			for _, family := range tdResp.Families {
				if desc, err := awscli.Run("ecs", "describe-task-definition",
					"--region", region, "--task-definition", family); err == nil {
					taskDefs = append(taskDefs, parseECSTaskDef(desc))
				}
			}
			// Attach task defs to first cluster (or all clusters if multiple)
			if len(clusters) > 0 && len(taskDefs) > 0 {
				clusters[0].TaskDefs = taskDefs
			}
		}
		// Enrich with services and running tasks per cluster
		for i := range clusters {
			cl := &clusters[i]
			// List services
			if svcData, err := awscli.Run("ecs", "list-services", "--region", region,
				"--cluster", cl.ClusterArn); err == nil {
				var svcResp struct {
					ServiceArns []string `json:"serviceArns"`
				}
				json.Unmarshal(svcData, &svcResp)
				if len(svcResp.ServiceArns) > 0 {
					args := append([]string{"ecs", "describe-services", "--region", region,
						"--cluster", cl.ClusterArn, "--services"}, svcResp.ServiceArns...)
					if descData, err := awscli.Run(args...); err == nil {
						var descResp struct {
							Services []json.RawMessage `json:"services"`
						}
						json.Unmarshal(descData, &descResp)
						for _, s := range descResp.Services {
							cl.ECSServices = append(cl.ECSServices, parseECSService(s))
						}
					}
				}
			}
			// List running tasks
			if taskData, err := awscli.Run("ecs", "list-tasks", "--region", region,
				"--cluster", cl.ClusterArn); err == nil {
				var taskResp struct {
					TaskArns []string `json:"taskArns"`
				}
				json.Unmarshal(taskData, &taskResp)
				if len(taskResp.TaskArns) > 0 {
					args := append([]string{"ecs", "describe-tasks", "--region", region,
						"--cluster", cl.ClusterArn, "--tasks"}, taskResp.TaskArns...)
					if descData, err := awscli.Run(args...); err == nil {
						var descResp struct {
							Tasks []json.RawMessage `json:"tasks"`
						}
						json.Unmarshal(descData, &descResp)
						for _, t := range descResp.Tasks {
							cl.Tasks = append(cl.Tasks, parseECSTask(t))
						}
					}
				}
			}
		}
		enriched, _ := json.Marshal(clusters)
		WriteCache(region+":ecs-enriched", enriched)
		results = append(results, SyncResult{Service: "ecs", Count: len(clusters)})
	} else {
		results = append(results, SyncResult{Service: "ecs", Error: err.Error()})
	}
	step("ecs")

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
	step("lambda")

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
		KeyName          string `json:"KeyName"`
		ImageId          string `json:"ImageId"`
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
		BlockDeviceMappings []struct {
			DeviceName string `json:"DeviceName"`
			Ebs        *struct {
				VolumeId string `json:"VolumeId"`
			} `json:"Ebs"`
		} `json:"BlockDeviceMappings"`
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
		KeyName:      r.KeyName,
		ImageId:      r.ImageId,
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
	for _, bdm := range r.BlockDeviceMappings {
		if bdm.Ebs != nil {
			inst.Volumes = append(inst.Volumes, EC2Volume{
				VolumeId:   bdm.Ebs.VolumeId,
				DeviceName: bdm.DeviceName,
			})
		}
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

func resolveRolePolicies(roleArn string) (roleName string, policies []string) {
	parts := strings.Split(roleArn, "/")
	roleName = parts[len(parts)-1]
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
	return
}

func parseECSTaskDef(raw json.RawMessage) ECSTaskDef {
	var r struct {
		TaskDefinition struct {
			Family               string   `json:"family"`
			Revision             int      `json:"revision"`
			TaskRoleArn          string   `json:"taskRoleArn"`
			ExecutionRoleArn     string   `json:"executionRoleArn"`
			RequiresCompatibilities []string `json:"requiresCompatibilities"`
		} `json:"taskDefinition"`
	}
	json.Unmarshal(raw, &r)

	td := ECSTaskDef{
		Family:   r.TaskDefinition.Family,
		Revision: r.TaskDefinition.Revision,
	}
	if len(r.TaskDefinition.RequiresCompatibilities) > 0 {
		td.LaunchType = r.TaskDefinition.RequiresCompatibilities[0]
	}
	if r.TaskDefinition.TaskRoleArn != "" {
		td.TaskRoleName, td.TaskRolePolicies = resolveRolePolicies(r.TaskDefinition.TaskRoleArn)
	}
	if r.TaskDefinition.ExecutionRoleArn != "" {
		td.ExecRoleName, td.ExecRolePolicies = resolveRolePolicies(r.TaskDefinition.ExecutionRoleArn)
	}
	return td
}

func parseECSService(raw json.RawMessage) ECSService {
	var r struct {
		ServiceName    string `json:"serviceName"`
		Status         string `json:"status"`
		DesiredCount   int    `json:"desiredCount"`
		RunningCount   int    `json:"runningCount"`
		LaunchType     string `json:"launchType"`
		TaskDefinition string `json:"taskDefinition"`
		NetworkConfiguration *struct {
			AwsvpcConfiguration struct {
				Subnets        []string `json:"subnets"`
				SecurityGroups []string `json:"securityGroups"`
				AssignPublicIp string   `json:"assignPublicIp"`
			} `json:"awsvpcConfiguration"`
		} `json:"networkConfiguration"`
		LoadBalancers []struct {
			TargetGroupArn string `json:"targetGroupArn"`
			ContainerName  string `json:"containerName"`
			ContainerPort  int    `json:"containerPort"`
		} `json:"loadBalancers"`
	}
	json.Unmarshal(raw, &r)

	svc := ECSService{
		ServiceName:    r.ServiceName,
		Status:         r.Status,
		DesiredCount:   r.DesiredCount,
		RunningCount:   r.RunningCount,
		LaunchType:     r.LaunchType,
		TaskDefinition: r.TaskDefinition,
	}
	if r.NetworkConfiguration != nil {
		svc.SubnetIds = r.NetworkConfiguration.AwsvpcConfiguration.Subnets
		svc.SecurityGroups = r.NetworkConfiguration.AwsvpcConfiguration.SecurityGroups
		svc.AssignPublicIP = r.NetworkConfiguration.AwsvpcConfiguration.AssignPublicIp == "ENABLED"
	}
	for _, lb := range r.LoadBalancers {
		svc.LBTargetGroups = append(svc.LBTargetGroups, lb.TargetGroupArn)
	}
	return svc
}

func parseECSTask(raw json.RawMessage) ECSTask {
	var r struct {
		TaskArn              string `json:"taskArn"`
		TaskDefinitionArn    string `json:"taskDefinitionArn"`
		LastStatus           string `json:"lastStatus"`
		LaunchType           string `json:"launchType"`
		Attachments []struct {
			Type    string `json:"type"`
			Details []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"details"`
		} `json:"attachments"`
	}
	json.Unmarshal(raw, &r)

	task := ECSTask{
		TaskArn:        r.TaskArn,
		TaskDefinition: r.TaskDefinitionArn,
		LastStatus:     r.LastStatus,
		LaunchType:     r.LaunchType,
	}
	// Extract IPs from ENI attachment details
	for _, att := range r.Attachments {
		if att.Type == "ElasticNetworkInterface" {
			for _, d := range att.Details {
				switch d.Name {
				case "privateIPv4Address":
					task.PrivateIP = d.Value
				case "publicIPv4Address":
					task.PublicIP = d.Value
				case "subnetId":
					task.SubnetId = d.Value
				}
			}
		}
	}
	return task
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

