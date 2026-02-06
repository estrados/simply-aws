package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/estrados/simply-aws/internal/sync"
)

// ANSI helpers
func bold(s string) string    { return "\033[1m" + s + "\033[0m" }
func dim(s string) string     { return "\033[2m" + s + "\033[0m" }
func cyan(s string) string    { return "\033[36m" + s + "\033[0m" }
func green(s string) string   { return "\033[32m" + s + "\033[0m" }
func yellow(s string) string  { return "\033[33m" + s + "\033[0m" }
func red(s string) string     { return "\033[31m" + s + "\033[0m" }
func magenta(s string) string { return "\033[35m" + s + "\033[0m" }

func truncID(id string, n int) string {
	if len(id) <= n {
		return id
	}
	return id[:n-3] + "..."
}

func header(title string) {
	line := strings.Repeat("━", 40)
	fmt.Printf("\n%s %s %s\n\n", bold("━━"), bold(title), dim(line[:40-len(title)]))
}

func printMenu(region string) {
	line := strings.Repeat("━", 35)
	fmt.Printf("\n%s %s %s\n\n", bold("simply-aws"), bold("━━"), dim(region+" "+line[:35-len(region)]))
	fmt.Printf("  %s  Region [%s]\n", bold("0"), cyan(region))
	fmt.Printf("  %s  Network\n", bold("1"))
	fmt.Printf("  %s  Compute\n", bold("2"))
	fmt.Printf("  %s  Database\n", bold("3"))
	fmt.Printf("  %s  S3 & Data\n", bold("4"))
	fmt.Printf("  %s  Queues & Streaming\n", bold("5"))
	fmt.Printf("  %s  AI & ML\n", bold("6"))
	fmt.Printf("  %s  IAM\n", bold("7"))
	fmt.Printf("  %s  Quit\n", bold("q"))
	fmt.Printf("\n%s ", bold("▸"))
}

func switchRegion(scanner *bufio.Scanner) string {
	regions, err := sync.GetEnabledRegions()
	if err != nil || len(regions) == 0 {
		fmt.Println(red("  No regions configured. Run 'saws up' and sync first."))
		return ""
	}
	fmt.Println()
	for i, r := range regions {
		fmt.Printf("  %s  %s\n", bold(fmt.Sprintf("%d", i+1)), r)
	}
	fmt.Printf("\n%s ", bold("▸"))
	if !scanner.Scan() {
		return ""
	}
	choice := strings.TrimSpace(scanner.Text())
	var idx int
	if _, err := fmt.Sscanf(choice, "%d", &idx); err == nil && idx >= 1 && idx <= len(regions) {
		return regions[idx-1]
	}
	return ""
}

// RunView starts the interactive CLI view loop.
func RunView(defaultRegion string) {
	region := defaultRegion
	scanner := bufio.NewScanner(os.Stdin)

	for {
		printMenu(region)
		if !scanner.Scan() {
			break
		}
		choice := strings.TrimSpace(scanner.Text())
		switch choice {
		case "0":
			if r := switchRegion(scanner); r != "" {
				region = r
			}
		case "1":
			printNetwork(region)
		case "2":
			printCompute(region)
		case "3":
			printDatabase(region)
		case "4":
			printS3(region)
		case "5":
			printStreaming(region)
		case "6":
			printAI(region)
		case "7":
			printIAM()
		case "q", "Q":
			return
		}
	}
}

// ── Network ──────────────────────────────────────────

func printNetwork(region string) {
	data, err := sync.LoadVPCData(region)
	if err != nil {
		fmt.Println(red("  Error loading network data: " + err.Error()))
		return
	}
	header("Network")

	if len(data.VPCs) == 0 {
		fmt.Println(dim("  No VPCs found"))
		return
	}

	for _, vpc := range data.VPCs {
		name := vpc.Name
		if name == "" {
			name = truncID(vpc.VpcId, 16)
		}
		if vpc.IsDefault {
			name += dim(" (default)")
		}
		fmt.Printf("%s  %-30s %s  %s\n", bold("VPC"), cyan(name), vpc.CidrBlock, green(vpc.State))

		// Subnets
		subnets := filterByVPC(data.Subnets, vpc.VpcId)
		if len(subnets) > 0 {
			fmt.Printf("├─ Subnets (%d)\n", len(subnets))
			for i, s := range subnets {
				prefix := "│  ├─"
				if i == len(subnets)-1 {
					prefix = "│  └─"
				}
				name := s.Name
				if name == "" {
					name = truncID(s.SubnetId, 16)
				}
				az := s.AvailabilityZone
				if len(az) > 2 {
					az = az[len(az)-2:]
				}
				fmt.Printf("%s %-22s %s  %s  %d IPs\n", prefix, cyan(name), s.CidrBlock, dim(az), s.AvailableIPs)
			}
		}

		// Security Groups
		sgs := filterSGsByVPC(data.SecurityGroups, vpc.VpcId)
		if len(sgs) > 0 {
			fmt.Printf("├─ Security Groups (%d)\n", len(sgs))
			for i, sg := range sgs {
				prefix := "│  ├─"
				if i == len(sgs)-1 {
					prefix = "│  └─"
				}
				name := sg.Name
				if name == "" {
					name = sg.GroupName
				}
				fmt.Printf("%s %-22s %d in / %d out\n", prefix, yellow(name), sg.InboundCount, sg.OutboundCount)
			}
		}

		// IGWs
		for _, igw := range data.IGWs {
			attached := false
			for _, vid := range igw.AttachedVpcIds {
				if vid == vpc.VpcId {
					attached = true
					break
				}
			}
			if attached {
				label := igw.Name
				if label == "" {
					label = truncID(igw.InternetGatewayId, 16)
				}
				fmt.Printf("├─ IGW  %s\n", cyan(label))
			}
		}

		// NAT Gateways
		for _, nat := range data.NATGWs {
			if nat.VpcId == vpc.VpcId {
				label := nat.Name
				if label == "" {
					label = truncID(nat.NatGatewayId, 16)
				}
				fmt.Printf("├─ NAT  %s  %s\n", cyan(label), green(nat.State))
			}
		}

		// Route Tables
		rts := filterRTsByVPC(data.RouteTables, vpc.VpcId)
		if len(rts) > 0 {
			fmt.Printf("├─ Route Tables (%d)\n", len(rts))
			for i, rt := range rts {
				prefix := "│  ├─"
				if i == len(rts)-1 {
					prefix := "   └─"
					_ = prefix
				}
				name := rt.Name
				if name == "" {
					name = truncID(rt.RouteTableId, 16)
				}
				kind := "custom"
				if rt.IsMain {
					kind = "main"
				}
				fmt.Printf("%s %-22s %-10s %d routes\n", prefix, cyan(name), dim(kind), len(rt.Routes))
			}
		}

		// Load Balancers
		lbs := filterLBsByVPC(data.LoadBalancers, vpc.VpcId)
		if len(lbs) > 0 {
			fmt.Printf("└─ Load Balancers (%d)\n", len(lbs))
			for i, lb := range lbs {
				prefix := "   ├─"
				if i == len(lbs)-1 {
					prefix = "   └─"
				}
				fmt.Printf("%s %-22s %-6s %s  %s\n", prefix, cyan(lb.Name), dim(lb.Type), dim(lb.Scheme), green(lb.State))
			}
		}

		fmt.Println()
	}
}

func filterByVPC(subnets []sync.Subnet, vpcId string) []sync.Subnet {
	var out []sync.Subnet
	for _, s := range subnets {
		if s.VpcId == vpcId {
			out = append(out, s)
		}
	}
	return out
}

func filterSGsByVPC(sgs []sync.SecurityGroup, vpcId string) []sync.SecurityGroup {
	var out []sync.SecurityGroup
	for _, sg := range sgs {
		if sg.VpcId == vpcId {
			out = append(out, sg)
		}
	}
	return out
}

func filterRTsByVPC(rts []sync.RouteTable, vpcId string) []sync.RouteTable {
	var out []sync.RouteTable
	for _, rt := range rts {
		if rt.VpcId == vpcId {
			out = append(out, rt)
		}
	}
	return out
}

func filterLBsByVPC(lbs []sync.LoadBalancer, vpcId string) []sync.LoadBalancer {
	var out []sync.LoadBalancer
	for _, lb := range lbs {
		if lb.VpcId == vpcId {
			out = append(out, lb)
		}
	}
	return out
}

// ── Compute ──────────────────────────────────────────

func printCompute(region string) {
	data, err := sync.LoadComputeData(region)
	if err != nil {
		fmt.Println(red("  Error loading compute data: " + err.Error()))
		return
	}
	header("Compute")

	// EC2
	if len(data.EC2) > 0 {
		fmt.Printf("%s (%d)\n", bold("EC2 Instances"), len(data.EC2))
		for i, inst := range data.EC2 {
			prefix := "├─"
			if i == len(data.EC2)-1 && len(data.ECS) == 0 && len(data.Lambda) == 0 {
				prefix = "└─"
			}
			name := inst.Name
			if name == "" {
				name = truncID(inst.InstanceId, 16)
			}
			stateColor := green
			if inst.State == "stopped" {
				stateColor = red
			} else if inst.State == "pending" || inst.State == "stopping" {
				stateColor = yellow
			}
			ip := inst.PrivateIP
			if inst.PublicIP != "" {
				ip = inst.PublicIP
			}
			fmt.Printf("%s %-24s %-14s %s  %s\n", prefix, cyan(name), dim(inst.InstanceType), stateColor(inst.State), dim(ip))
		}
		fmt.Println()
	}

	// ECS
	if len(data.ECS) > 0 {
		fmt.Printf("%s (%d)\n", bold("ECS Clusters"), len(data.ECS))
		for _, cluster := range data.ECS {
			fmt.Printf("├─ %s  %s  %d svc  %d tasks\n",
				cyan(cluster.ClusterName), green(cluster.Status),
				cluster.Services, cluster.RunningTasks)
			for j, svc := range cluster.ECSServices {
				prefix := "│  ├─"
				if j == len(cluster.ECSServices)-1 && len(cluster.Tasks) == 0 {
					prefix = "│  └─"
				}
				fmt.Printf("%s svc %s  %d/%d  %s\n", prefix,
					yellow(svc.ServiceName), svc.RunningCount, svc.DesiredCount, dim(svc.LaunchType))
			}
			for j, task := range cluster.Tasks {
				prefix := "│  ├─"
				if j == len(cluster.Tasks)-1 {
					prefix = "│  └─"
				}
				fmt.Printf("%s task %s  %s  %s\n", prefix,
					dim(truncID(task.TaskArn, 16)), task.LastStatus, dim(task.LaunchType))
			}
		}
		fmt.Println()
	}

	// Lambda
	if len(data.Lambda) > 0 {
		fmt.Printf("%s (%d)\n", bold("Lambda Functions"), len(data.Lambda))
		for i, fn := range data.Lambda {
			prefix := "├─"
			if i == len(data.Lambda)-1 {
				prefix = "└─"
			}
			runtime := fn.Runtime
			if runtime == "" {
				runtime = "container"
			}
			fmt.Printf("%s %-30s %-14s %dMB  %ds\n", prefix,
				cyan(fn.FunctionName), dim(runtime), fn.MemorySize, fn.Timeout)
		}
		fmt.Println()
	}

	if len(data.EC2) == 0 && len(data.ECS) == 0 && len(data.Lambda) == 0 {
		fmt.Println(dim("  No compute resources found"))
	}
}

// ── Database ─────────────────────────────────────────

func printDatabase(region string) {
	data, err := sync.LoadDatabaseData(region)
	if err != nil {
		fmt.Println(red("  Error loading database data: " + err.Error()))
		return
	}
	header("Database")

	if len(data.RDS) > 0 {
		fmt.Printf("%s (%d)\n", bold("RDS Instances"), len(data.RDS))
		for i, db := range data.RDS {
			prefix := "├─"
			if i == len(data.RDS)-1 && len(data.DynamoDB) == 0 && len(data.ElastiCache) == 0 {
				prefix = "└─"
			}
			multiAZ := ""
			if db.MultiAZ {
				multiAZ = " multi-az"
			}
			fmt.Printf("%s %-28s %-10s %-14s %s%s\n", prefix,
				cyan(db.DBInstanceId), dim(db.Engine+" "+db.EngineVersion),
				dim(db.InstanceClass), green(db.Status), dim(multiAZ))
		}
		fmt.Println()
	}

	if len(data.DynamoDB) > 0 {
		fmt.Printf("%s (%d)\n", bold("DynamoDB Tables"), len(data.DynamoDB))
		for i, t := range data.DynamoDB {
			prefix := "├─"
			if i == len(data.DynamoDB)-1 && len(data.ElastiCache) == 0 {
				prefix = "└─"
			}
			size := formatBytes(t.SizeBytes)
			fmt.Printf("%s %-28s %d items  %s  %s\n", prefix,
				cyan(t.TableName), t.ItemCount, dim(size), green(t.Status))
		}
		fmt.Println()
	}

	if len(data.ElastiCache) > 0 {
		fmt.Printf("%s (%d)\n", bold("ElastiCache"), len(data.ElastiCache))
		for i, c := range data.ElastiCache {
			prefix := "├─"
			if i == len(data.ElastiCache)-1 {
				prefix = "└─"
			}
			fmt.Printf("%s %-28s %-10s %-14s %s\n", prefix,
				cyan(c.CacheClusterId), dim(c.Engine+" "+c.EngineVersion),
				dim(c.CacheNodeType), green(c.Status))
		}
		fmt.Println()
	}

	if len(data.RDS) == 0 && len(data.DynamoDB) == 0 && len(data.ElastiCache) == 0 {
		fmt.Println(dim("  No database resources found"))
	}
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// ── S3 & Data ────────────────────────────────────────

func printS3(region string) {
	header("S3 & Data")

	s3data, err := sync.LoadS3DataEnriched()
	if err != nil {
		s3data, err = sync.LoadS3Data()
	}
	if err == nil && len(s3data.Buckets) > 0 {
		fmt.Printf("%s (%d)\n", bold("S3 Buckets"), len(s3data.Buckets))
		for i, b := range s3data.Buckets {
			prefix := "├─"
			if i == len(s3data.Buckets)-1 {
				prefix = "└─"
			}
			access := green("private")
			if b.PolicyPublic || b.ACLPublic {
				access = red("PUBLIC")
			} else if b.Access != "" && b.Access != "private" {
				access = yellow(b.Access)
			}
			ver := ""
			if b.Versioning == "Enabled" {
				ver = " " + dim("versioned")
			}
			fmt.Printf("%s %-36s %s  %s%s\n", prefix, cyan(b.Name), dim(b.Region), access, ver)
		}
		fmt.Println()
	} else if err != nil {
		fmt.Println(dim("  No S3 data cached"))
	}

	// Data warehouse
	dw, err := sync.LoadDataWarehouseData(region)
	if err != nil {
		return
	}

	if len(dw.Redshift) > 0 {
		fmt.Printf("%s (%d)\n", bold("Redshift Clusters"), len(dw.Redshift))
		for i, c := range dw.Redshift {
			prefix := "├─"
			if i == len(dw.Redshift)-1 {
				prefix = "└─"
			}
			fmt.Printf("%s %-28s %-14s %d nodes  %s\n", prefix,
				cyan(c.ClusterIdentifier), dim(c.NodeType), c.NumberOfNodes, green(c.Status))
		}
		fmt.Println()
	}

	if len(dw.Athena) > 0 {
		fmt.Printf("%s (%d)\n", bold("Athena Workgroups"), len(dw.Athena))
		for i, a := range dw.Athena {
			prefix := "├─"
			if i == len(dw.Athena)-1 {
				prefix = "└─"
			}
			fmt.Printf("%s %-28s %s\n", prefix, cyan(a.Name), green(a.State))
		}
		fmt.Println()
	}

	if len(dw.Glue) > 0 {
		fmt.Printf("%s (%d)\n", bold("Glue Databases"), len(dw.Glue))
		for i, g := range dw.Glue {
			prefix := "├─"
			if i == len(dw.Glue)-1 {
				prefix = "└─"
			}
			fmt.Printf("%s %-28s %s\n", prefix, cyan(g.Name), dim(g.Description))
		}
		fmt.Println()
	}

	if (s3data == nil || len(s3data.Buckets) == 0) && len(dw.Redshift) == 0 && len(dw.Athena) == 0 && len(dw.Glue) == 0 {
		fmt.Println(dim("  No S3 or data resources found"))
	}
}

// ── Queues & Streaming ───────────────────────────────

func printStreaming(region string) {
	data, err := sync.LoadStreamingData(region)
	if err != nil {
		fmt.Println(red("  Error loading streaming data: " + err.Error()))
		return
	}
	header("Queues & Streaming")

	if len(data.SQS) > 0 {
		fmt.Printf("%s (%d)\n", bold("SQS Queues"), len(data.SQS))
		for i, q := range data.SQS {
			prefix := "├─"
			if i == len(data.SQS)-1 && len(data.SNS) == 0 && len(data.Kinesis) == 0 && len(data.EventBridge) == 0 {
				prefix = "└─"
			}
			fifo := ""
			if q.IsFIFO {
				fifo = dim(" FIFO")
			}
			fmt.Printf("%s %-34s ~%s msgs%s\n", prefix, cyan(q.QueueName), q.ApproximateMessages, fifo)
		}
		fmt.Println()
	}

	if len(data.SNS) > 0 {
		fmt.Printf("%s (%d)\n", bold("SNS Topics"), len(data.SNS))
		for i, t := range data.SNS {
			prefix := "├─"
			if i == len(data.SNS)-1 && len(data.Kinesis) == 0 && len(data.EventBridge) == 0 {
				prefix = "└─"
			}
			fmt.Printf("%s %-34s %d subs\n", prefix, cyan(t.Name), t.Subscriptions)
		}
		fmt.Println()
	}

	if len(data.Kinesis) > 0 {
		fmt.Printf("%s (%d)\n", bold("Kinesis Streams"), len(data.Kinesis))
		for i, s := range data.Kinesis {
			prefix := "├─"
			if i == len(data.Kinesis)-1 && len(data.EventBridge) == 0 {
				prefix = "└─"
			}
			fmt.Printf("%s %-34s %d shards  %dh retention  %s\n", prefix,
				cyan(s.StreamName), s.ShardCount, s.Retention, green(s.StreamStatus))
		}
		fmt.Println()
	}

	if len(data.EventBridge) > 0 {
		fmt.Printf("%s (%d)\n", bold("EventBridge Buses"), len(data.EventBridge))
		for i, b := range data.EventBridge {
			prefix := "├─"
			if i == len(data.EventBridge)-1 {
				prefix = "└─"
			}
			fmt.Printf("%s %-34s %d rules\n", prefix, cyan(b.Name), len(b.Rules))
			for j, r := range b.Rules {
				rprefix := "│  ├─"
				if j == len(b.Rules)-1 {
					rprefix = "│  └─"
				}
				sched := ""
				if r.Schedule != "" {
					sched = " " + dim(r.Schedule)
				}
				fmt.Printf("%s %-30s %s%s\n", rprefix, yellow(r.Name), green(r.State), sched)
			}
		}
		fmt.Println()
	}

	if len(data.SQS) == 0 && len(data.SNS) == 0 && len(data.Kinesis) == 0 && len(data.EventBridge) == 0 {
		fmt.Println(dim("  No streaming resources found"))
	}
}

// ── AI & ML ──────────────────────────────────────────

func printAI(region string) {
	data, err := sync.LoadAIData(region)
	if err != nil {
		fmt.Println(red("  Error loading AI data: " + err.Error()))
		return
	}
	header("AI & ML")

	if len(data.SageMakerNotebooks) > 0 {
		fmt.Printf("%s (%d)\n", bold("SageMaker Notebooks"), len(data.SageMakerNotebooks))
		for i, nb := range data.SageMakerNotebooks {
			prefix := "├─"
			if i == len(data.SageMakerNotebooks)-1 && len(data.SageMakerEndpoints) == 0 && len(data.SageMakerModels) == 0 && len(data.BedrockModels) == 0 {
				prefix = "└─"
			}
			stateColor := green
			if nb.Status != "InService" {
				stateColor = yellow
			}
			fmt.Printf("%s %-28s %-14s %s\n", prefix, cyan(nb.Name), dim(nb.InstanceType), stateColor(nb.Status))
		}
		fmt.Println()
	}

	if len(data.SageMakerEndpoints) > 0 {
		fmt.Printf("%s (%d)\n", bold("SageMaker Endpoints"), len(data.SageMakerEndpoints))
		for i, ep := range data.SageMakerEndpoints {
			prefix := "├─"
			if i == len(data.SageMakerEndpoints)-1 && len(data.SageMakerModels) == 0 && len(data.BedrockModels) == 0 {
				prefix = "└─"
			}
			fmt.Printf("%s %-28s %-14s %dx  %s\n", prefix,
				cyan(ep.Name), dim(ep.InstanceType), ep.InstanceCount, green(ep.Status))
		}
		fmt.Println()
	}

	if len(data.SageMakerModels) > 0 {
		fmt.Printf("%s (%d)\n", bold("SageMaker Models"), len(data.SageMakerModels))
		for i, m := range data.SageMakerModels {
			prefix := "├─"
			if i == len(data.SageMakerModels)-1 && len(data.BedrockModels) == 0 {
				prefix = "└─"
			}
			fmt.Printf("%s %s\n", prefix, cyan(m.Name))
		}
		fmt.Println()
	}

	if len(data.BedrockModels) > 0 {
		// Group by provider
		providers := make(map[string][]sync.BedrockModel)
		var order []string
		for _, m := range data.BedrockModels {
			if _, seen := providers[m.Provider]; !seen {
				order = append(order, m.Provider)
			}
			providers[m.Provider] = append(providers[m.Provider], m)
		}
		fmt.Printf("%s (%d)\n", bold("Bedrock Models"), len(data.BedrockModels))
		for pi, prov := range order {
			models := providers[prov]
			prefix := "├─"
			if pi == len(order)-1 && len(data.BedrockCustom) == 0 {
				prefix = "└─"
			}
			fmt.Printf("%s %s (%d)\n", prefix, magenta(prov), len(models))
			for j, m := range models {
				mprefix := "│  ├─"
				if j == len(models)-1 {
					mprefix = "│  └─"
				}
				fmt.Printf("%s %s\n", mprefix, dim(m.ModelId))
			}
		}
		fmt.Println()
	}

	if len(data.BedrockCustom) > 0 {
		fmt.Printf("%s (%d)\n", bold("Bedrock Custom Models"), len(data.BedrockCustom))
		for i, m := range data.BedrockCustom {
			prefix := "├─"
			if i == len(data.BedrockCustom)-1 {
				prefix = "└─"
			}
			fmt.Printf("%s %-28s base: %s\n", prefix, cyan(m.ModelName), dim(m.BaseModelId))
		}
		fmt.Println()
	}

	if len(data.SageMakerNotebooks) == 0 && len(data.SageMakerEndpoints) == 0 &&
		len(data.SageMakerModels) == 0 && len(data.BedrockModels) == 0 && len(data.BedrockCustom) == 0 {
		fmt.Println(dim("  No AI/ML resources found"))
	}
}

// ── IAM ──────────────────────────────────────────────

func printIAM() {
	data, err := sync.LoadIAMData()
	if err != nil {
		fmt.Println(red("  Error loading IAM data: " + err.Error()))
		return
	}
	header("IAM")

	if len(data.Roles) > 0 {
		// Group roles by principal
		type roleGroup struct {
			principal string
			roles     []sync.IAMRole
		}
		groups := make(map[string]*roleGroup)
		var order []string
		for _, r := range data.Roles {
			principal := "Other"
			if len(r.TrustPolicy) > 0 {
				principal = r.TrustPolicy[0].Principal
			}
			if principal == "" {
				principal = "Other"
			}
			if _, ok := groups[principal]; !ok {
				groups[principal] = &roleGroup{principal: principal}
				order = append(order, principal)
			}
			groups[principal].roles = append(groups[principal].roles, r)
		}

		fmt.Printf("%s (%d)\n", bold("Roles"), len(data.Roles))
		for gi, key := range order {
			g := groups[key]
			prefix := "├─"
			if gi == len(order)-1 && len(data.Groups) == 0 {
				prefix = "└─"
			}
			fmt.Printf("%s %s (%d)\n", prefix, magenta(g.principal), len(g.roles))
			for ri, r := range g.roles {
				rprefix := "│  ├─"
				if ri == len(g.roles)-1 {
					rprefix = "│  └─"
				}
				policies := len(r.AttachedPolicies) + len(r.InlinePolicies)
				svcLinked := ""
				if r.IsServiceLinked {
					svcLinked = dim(" svc-linked")
				}
				fmt.Printf("%s %-34s %d policies%s\n", rprefix, cyan(r.RoleName), policies, svcLinked)
			}
		}
		fmt.Println()
	}

	if len(data.Groups) > 0 {
		fmt.Printf("%s (%d)\n", bold("Groups"), len(data.Groups))
		for i, g := range data.Groups {
			prefix := "├─"
			if i == len(data.Groups)-1 {
				prefix = "└─"
			}
			policies := len(g.AttachedPolicies) + len(g.InlinePolicies)
			fmt.Printf("%s %-28s %d members  %d policies\n", prefix,
				cyan(g.GroupName), len(g.Members), policies)
		}
		fmt.Println()
	}

	if len(data.Roles) == 0 && len(data.Groups) == 0 {
		fmt.Println(dim("  No IAM data cached"))
	}
}
