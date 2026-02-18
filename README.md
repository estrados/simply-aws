# saws : simply aws

**Your entire AWS infrastructure, one page, offline-ready.**

### Web Dashboard

![saws web dashboard](docs/saws-web.gif)

### CLI View

![saws cli view](docs/saws-cli.gif)

## The Problem

You're an engineer or architect responsible for AWS infrastructure across multiple services and regions. Reviewing what's actually running should be simple — but it isn't.

**The AWS console is built for everything, which makes it slow for anything.**

- Checking your VPCs means one page. Security groups, another. EC2 instances, another. Each page takes 5-10 seconds to load, and a 10-step review burns an hour.
- You're working from home on spotty WiFi, or at a client's office with throttled internet. The console becomes nearly unusable — every click is a loading spinner.
- There's no single view that shows how your EC2 connects to its VPC, subnet, security groups, and IAM role. You're mentally stitching together information across 6 different pages.
- Onboarding onto a new project means clicking through dozens of console pages just to understand what exists. "What's running in this account?" shouldn't take 30 minutes to answer.
- Even the region selector works against you. The console shows all 30+ regions, you can't hide the ones you don't use, and every service page defaults to whatever region you're in. You're constantly double-checking "wait, am I looking at the right region?"

The console has every bell and whistle — it has to. But when you just need to see your infrastructure and understand how it connects, all that complexity gets in the way.

**It's not just for infrastructure engineers.** Even if you're a backend dev, a team lead, or new to AWS — `saws` lowers the barrier. It makes you more aware of what's actually running, easier to review after every infrastructure change, and faster to spot what shifted between deploys. You don't need to be an AWS expert to understand your own account.

## The Solution

`saws` syncs your AWS data once, caches it locally, and gives you a fast single-page dashboard that works instantly — even offline.

```bash
saws up
# Open http://localhost:3131
# Click sync, then browse everything locally
```

**One sync. Then everything is local, fast, and connected.**

## Features

- **7 resource tabs** — Network, Compute, Database, S3 & Data, Queues & Streaming, AI & ML, IAM
- **40+ resource types** — VPCs, EC2, ECS, Lambda, RDS, DynamoDB, S3, SQS, SNS, SageMaker, Bedrock, IAM roles, and more
- **Connected resources** — click an EC2 instance and see its VPC, subnet, security groups, IAM role, and policies in one view
- **Multi-region** — enable only the regions you use, hide the rest. No more scrolling through 30+ regions you'll never touch
- **Async sync with live progress** — non-blocking, shows what's syncing in real-time
- **Offline after first sync** — all data cached in local SQLite, no internet needed to browse
- **CLI view & sync** — `saws view` for terminal UI, `saws sync` to pull data without a browser
- **Single binary** — no Docker, no Node.js, no cloud dependencies beyond AWS CLI

### Resource Coverage

| Tab | Services |
|-----|----------|
| **Network** | VPCs, Subnets, Security Groups, IGWs, NAT Gateways, Route Tables, ALBs, NLBs, Target Groups |
| **Compute** | EC2 Instances, ECS Clusters/Services/Tasks/Task Definitions, Lambda Functions |
| **Database** | RDS, DynamoDB, ElastiCache |
| **S3 & Data** | S3 Buckets, Redshift, Athena Workgroups, Glue Databases |
| **Streaming** | SQS Queues, SNS Topics, Kinesis Streams, EventBridge Buses |
| **AI & ML** | SageMaker Notebooks/Endpoints/Models, Bedrock Foundation & Custom Models |
| **IAM** | Roles (grouped by trust principal), Groups, Policies |

## Installation

### Prerequisites

- [AWS CLI v2](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html) installed and configured (`aws configure`)

### From source

```bash
git clone https://github.com/estrados/simply-aws.git
cd simply-aws
make build
./saws up
```

### Go install

```bash
go install github.com/estrados/simply-aws/cmd/saws@latest
saws up
```

## Usage

```bash
# Start the web dashboard (default port 3131)
saws up

# Custom port
saws up --port 8080

# Sync from the terminal
saws sync
saws sync --region us-west-2

# Interactive CLI view (no browser needed)
saws view
saws view --region ap-southeast-1
```

### Web Dashboard

Open [http://localhost:3131](http://localhost:3131), hit sync, and you're set.

1. **Sync** — click the refresh button to pull data from AWS (or "Sync all" for everything)
2. **Browse** — navigate tabs, click any resource row for full details
3. **Switch regions** — use the dropdown to jump between regions
4. **Work offline** — close your VPN, disconnect WiFi — your data is cached locally

### CLI View

Run `saws view` for an interactive terminal UI — pick a tab (1-7) to see resources, option 0 to switch region. Reads from the same SQLite cache as the web dashboard.

## How It Works

```
┌────────────┐   ┌────────────┐   ┌────────────┐
│   Browser  │──>│            │──>│  AWS CLI   │
│   (HTMX)   │<──│    saws    │<──│   (sync)   │
└────────────┘   │    (Go)    │   └────────────┘
                 │            │
┌────────────┐   │            │
│  Terminal  │──>│            │
│ (view/sync)│<──│            │
└────────────┘   └─────┬──────┘
                       │
                 ┌─────┴──────┐
                 │   SQLite   │
                 │   (.saws/) │
                 └────────────┘
```

1. **Sync** — `saws` calls AWS CLI commands, parses the JSON, enriches it (resolves IAM roles, links resources), and stores it in SQLite
2. **Serve** — Go templates + HTMX render a reactive UI with zero JavaScript frameworks
3. **Cache** — everything lives in `.saws/saws.db`. Restart `saws`, switch networks, go offline — your data is still there

### Why AWS CLI instead of the SDK?

- No AWS SDK dependency or credential chain complexity
- Uses your existing `aws configure` setup — same profiles, same SSO, same MFA
- If `aws sts get-caller-identity` works, `saws` works

## Development

```bash
# Hot-reload development (auto-installs 'air')
make dev

# Build binary
make build

# Run tests
go test ./...
```

### Project Structure

```
cmd/saws/           CLI entrypoint (cobra)
internal/
  awscli/           AWS CLI detection and subprocess execution
  cli/              Terminal UI (view, sync commands)
  server/           HTTP handlers, template rendering, routing
  sync/             Data models, AWS sync, SQLite cache, progress tracking
  cfn/              CloudFormation template parsing
web/
  templates/        Go HTML templates (one per tab + detail panels)
  styles.css        Single stylesheet, no build step
```

## Tech Stack

| Layer | Choice | Why |
|-------|--------|-----|
| Server | **Go** | Fast, single binary, great concurrency for parallel syncs |
| UI | **HTMX** | Server-rendered HTML with reactive updates, no JS build step |
| Cache | **SQLite (WAL)** | Embedded, zero-config, survives restarts |
| Data | **AWS CLI** | No SDK, uses existing credentials, works with SSO/MFA |

## License

MIT
