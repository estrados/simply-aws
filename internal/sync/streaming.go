package sync

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/estrados/simply-aws/internal/awscli"
)

type StreamingData struct {
	SQS         []SQSQueue         `json:"sqs"`
	SNS         []SNSTopic         `json:"sns"`
	Kinesis     []KinesisStream    `json:"kinesis"`
	EventBridge []EventBridgeBus   `json:"eventbridge"`
}

type SQSQueue struct {
	QueueName                string `json:"QueueName"`
	QueueUrl                 string `json:"QueueUrl"`
	Arn                      string `json:"Arn"`
	ApproximateMessages      string `json:"ApproximateMessages"`
	ApproximateMessagesNotVisible string `json:"ApproximateMessagesNotVisible"`
	VisibilityTimeout        string `json:"VisibilityTimeout"`
	MaxMessageSize           string `json:"MaxMessageSize"`
	MessageRetention         string `json:"MessageRetention"`
	CreatedTimestamp         string `json:"CreatedTimestamp"`
	DelaySeconds             string `json:"DelaySeconds"`
	IsFIFO                   bool   `json:"IsFIFO"`
	RedrivePolicy            string `json:"RedrivePolicy"`
	Policies                 []ResourcePolicy `json:"Policies"`
}

type SNSTopic struct {
	TopicArn      string           `json:"TopicArn"`
	Name          string           `json:"Name"`
	DisplayName   string           `json:"DisplayName"`
	Subscriptions int              `json:"Subscriptions"`
	Policies      []ResourcePolicy `json:"Policies"`
}

type KinesisStream struct {
	StreamName   string `json:"StreamName"`
	StreamARN    string `json:"StreamARN"`
	StreamStatus string `json:"StreamStatus"`
	StreamMode   string `json:"StreamMode"`
	ShardCount   int    `json:"ShardCount"`
	Retention    int    `json:"RetentionPeriodHours"`
	Encryption   string `json:"EncryptionType"`
	CreatedAt    string `json:"CreatedAt"`
}

type EventBridgeBus struct {
	Name      string             `json:"Name"`
	Arn       string             `json:"Arn"`
	Rules     []EventBridgeRule  `json:"Rules"`
}

type EventBridgeRule struct {
	Name        string `json:"Name"`
	State       string `json:"State"`
	Description string `json:"Description"`
	Schedule    string `json:"ScheduleExpression"`
}

func SyncStreamingData(region string) ([]SyncResult, error) {
	var results []SyncResult
	data := &StreamingData{}

	// SQS
	if raw, err := awscli.Run("sqs", "list-queues", "--region", region); err == nil {
		WriteCache(region+":sqs", raw)
		var resp struct {
			QueueUrls []string `json:"QueueUrls"`
		}
		json.Unmarshal(raw, &resp)

		for _, url := range resp.QueueUrls {
			queue := SQSQueue{QueueUrl: url}
			// Extract name from URL
			parts := strings.Split(url, "/")
			if len(parts) > 0 {
				queue.QueueName = parts[len(parts)-1]
			}
			queue.IsFIFO = strings.HasSuffix(queue.QueueName, ".fifo")

			// Get attributes
			if attrData, err := awscli.Run("sqs", "get-queue-attributes", "--queue-url", url,
				"--attribute-names", "All", "--region", region); err == nil {
				var attrResp struct {
					Attributes map[string]string `json:"Attributes"`
				}
				json.Unmarshal(attrData, &attrResp)
				a := attrResp.Attributes
				queue.Arn = a["QueueArn"]
				queue.ApproximateMessages = a["ApproximateNumberOfMessages"]
				queue.ApproximateMessagesNotVisible = a["ApproximateNumberOfMessagesNotVisible"]
				queue.VisibilityTimeout = a["VisibilityTimeoutSeconds"]
				queue.MaxMessageSize = a["MaximumMessageSize"]
				queue.MessageRetention = a["MessageRetentionPeriod"]
				queue.DelaySeconds = a["DelaySeconds"]
				queue.RedrivePolicy = a["RedrivePolicy"]
				if ts := a["CreatedTimestamp"]; ts != "" {
					queue.CreatedTimestamp = formatUnixTimestamp(ts)
				}
				if policy := a["Policy"]; policy != "" {
					queue.Policies = ParseResourcePolicies(policy)
				}
			}
			data.SQS = append(data.SQS, queue)
		}
		results = append(results, SyncResult{Service: "sqs", Count: len(resp.QueueUrls)})
	} else {
		results = append(results, SyncResult{Service: "sqs", Error: err.Error()})
	}

	// SNS
	if raw, err := awscli.Run("sns", "list-topics", "--region", region); err == nil {
		WriteCache(region+":sns", raw)
		var resp struct {
			Topics []struct {
				TopicArn string `json:"TopicArn"`
			} `json:"Topics"`
		}
		json.Unmarshal(raw, &resp)

		for _, t := range resp.Topics {
			topic := SNSTopic{TopicArn: t.TopicArn}
			// Extract name from ARN
			parts := strings.Split(t.TopicArn, ":")
			if len(parts) > 0 {
				topic.Name = parts[len(parts)-1]
			}

			// Get attributes
			if attrData, err := awscli.Run("sns", "get-topic-attributes", "--topic-arn", t.TopicArn,
				"--region", region); err == nil {
				var attrResp struct {
					Attributes map[string]string `json:"Attributes"`
				}
				json.Unmarshal(attrData, &attrResp)
				a := attrResp.Attributes
				topic.DisplayName = a["DisplayName"]
				if policy := a["Policy"]; policy != "" {
					topic.Policies = ParseResourcePolicies(policy)
				}
			}

			// Subscription count
			if subData, err := awscli.Run("sns", "list-subscriptions-by-topic", "--topic-arn", t.TopicArn,
				"--region", region); err == nil {
				var subResp struct {
					Subscriptions []json.RawMessage `json:"Subscriptions"`
				}
				json.Unmarshal(subData, &subResp)
				topic.Subscriptions = len(subResp.Subscriptions)
			}

			data.SNS = append(data.SNS, topic)
		}
		results = append(results, SyncResult{Service: "sns", Count: len(resp.Topics)})
	} else {
		results = append(results, SyncResult{Service: "sns", Error: err.Error()})
	}

	// Kinesis
	if raw, err := awscli.Run("kinesis", "list-streams", "--region", region); err == nil {
		WriteCache(region+":kinesis", raw)
		var resp struct {
			StreamSummaries []struct {
				StreamName   string `json:"StreamName"`
				StreamARN    string `json:"StreamARN"`
				StreamStatus string `json:"StreamStatus"`
				StreamModeDetails struct {
					StreamMode string `json:"StreamMode"`
				} `json:"StreamModeDetails"`
				StreamCreationTimestamp float64 `json:"StreamCreationTimestamp"`
			} `json:"StreamSummaries"`
		}
		json.Unmarshal(raw, &resp)

		for _, s := range resp.StreamSummaries {
			stream := KinesisStream{
				StreamName:   s.StreamName,
				StreamARN:    s.StreamARN,
				StreamStatus: s.StreamStatus,
				StreamMode:   s.StreamModeDetails.StreamMode,
			}
			if s.StreamCreationTimestamp > 0 {
				t := time.Unix(int64(s.StreamCreationTimestamp), 0)
				stream.CreatedAt = t.Format("2006-01-02 15:04")
			}

			// Get details
			if descData, err := awscli.Run("kinesis", "describe-stream-summary",
				"--stream-name", s.StreamName, "--region", region); err == nil {
				var descResp struct {
					StreamDescriptionSummary struct {
						OpenShardCount       int    `json:"OpenShardCount"`
						RetentionPeriodHours int    `json:"RetentionPeriodHours"`
						EncryptionType       string `json:"EncryptionType"`
					} `json:"StreamDescriptionSummary"`
				}
				json.Unmarshal(descData, &descResp)
				d := descResp.StreamDescriptionSummary
				stream.ShardCount = d.OpenShardCount
				stream.Retention = d.RetentionPeriodHours
				stream.Encryption = d.EncryptionType
			}

			data.Kinesis = append(data.Kinesis, stream)
		}
		results = append(results, SyncResult{Service: "kinesis", Count: len(resp.StreamSummaries)})
	} else {
		results = append(results, SyncResult{Service: "kinesis", Error: err.Error()})
	}

	// EventBridge
	if raw, err := awscli.Run("events", "list-event-buses", "--region", region); err == nil {
		WriteCache(region+":eventbridge", raw)
		var resp struct {
			EventBuses []struct {
				Name string `json:"Name"`
				Arn  string `json:"Arn"`
			} `json:"EventBuses"`
		}
		json.Unmarshal(raw, &resp)

		for _, b := range resp.EventBuses {
			bus := EventBridgeBus{Name: b.Name, Arn: b.Arn}

			// Get rules for this bus
			if rulesData, err := awscli.Run("events", "list-rules",
				"--event-bus-name", b.Name, "--region", region); err == nil {
				var rulesResp struct {
					Rules []struct {
						Name               string `json:"Name"`
						State              string `json:"State"`
						Description        string `json:"Description"`
						ScheduleExpression string `json:"ScheduleExpression"`
					} `json:"Rules"`
				}
				json.Unmarshal(rulesData, &rulesResp)
				for _, r := range rulesResp.Rules {
					bus.Rules = append(bus.Rules, EventBridgeRule{
						Name:        r.Name,
						State:       r.State,
						Description: r.Description,
						Schedule:    r.ScheduleExpression,
					})
				}
			}

			data.EventBridge = append(data.EventBridge, bus)
		}
		results = append(results, SyncResult{Service: "eventbridge", Count: len(resp.EventBuses)})
	} else {
		results = append(results, SyncResult{Service: "eventbridge", Error: err.Error()})
	}

	// Cache enriched data
	enriched, _ := json.Marshal(data)
	WriteCache(region+":streaming-enriched", enriched)

	return results, nil
}

func LoadStreamingData(region string) (*StreamingData, error) {
	raw, err := ReadCache(region + ":streaming-enriched")
	if err != nil || raw == nil {
		return nil, err
	}
	var data StreamingData
	json.Unmarshal(raw, &data)
	return &data, nil
}

func formatUnixTimestamp(ts string) string {
	var sec int64
	for _, c := range ts {
		if c >= '0' && c <= '9' {
			sec = sec*10 + int64(c-'0')
		} else {
			break
		}
	}
	if sec > 0 {
		t := time.Unix(sec, 0)
		return t.Format("2006-01-02 15:04")
	}
	return ts
}
