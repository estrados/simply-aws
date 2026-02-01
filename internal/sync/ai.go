package sync

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/estrados/simply-aws/internal/awscli"
)

type AIData struct {
	SageMakerNotebooks []SageMakerNotebook `json:"sagemakerNotebooks"`
	SageMakerEndpoints []SageMakerEndpoint `json:"sagemakerEndpoints"`
	SageMakerModels    []SageMakerModel    `json:"sagemakerModels"`
	BedrockModels      []BedrockModel      `json:"bedrockModels"`
	BedrockCustom      []BedrockCustomModel `json:"bedrockCustom"`
}

type SageMakerNotebook struct {
	Name             string `json:"Name"`
	Status           string `json:"Status"`
	InstanceType     string `json:"InstanceType"`
	CreationTime     string `json:"CreationTime"`
	Url              string `json:"Url"`
	DirectInternetAccess string `json:"DirectInternetAccess"`
	SubnetId         string `json:"SubnetId"`
	SecurityGroups   []string `json:"SecurityGroups"`
	RoleArn          string `json:"RoleArn"`
	RoleName         string `json:"RoleName"`
	VolumeSizeGB     int    `json:"VolumeSizeGB"`
}

type SageMakerEndpoint struct {
	Name         string `json:"Name"`
	Status       string `json:"Status"`
	CreationTime string `json:"CreationTime"`
	ModelName    string `json:"ModelName"`
	InstanceType string `json:"InstanceType"`
	InstanceCount int   `json:"InstanceCount"`
}

type SageMakerModel struct {
	Name         string `json:"Name"`
	CreationTime string `json:"CreationTime"`
	RoleArn      string `json:"RoleArn"`
	RoleName     string `json:"RoleName"`
}

type BedrockModel struct {
	ModelId      string `json:"ModelId"`
	ModelName    string `json:"ModelName"`
	Provider     string `json:"Provider"`
	InputModes   []string `json:"InputModes"`
	OutputModes  []string `json:"OutputModes"`
	Streaming    bool   `json:"Streaming"`
}

type BedrockCustomModel struct {
	ModelName    string `json:"ModelName"`
	ModelArn     string `json:"ModelArn"`
	BaseModelId  string `json:"BaseModelId"`
	CreationTime string `json:"CreationTime"`
}

func SyncAIData(region string, onStep ...func(string)) ([]SyncResult, error) {
	step := func(label string) {
		if len(onStep) > 0 && onStep[0] != nil {
			onStep[0](label)
		}
	}
	var results []SyncResult

	// SageMaker Notebook Instances
	if data, err := awscli.Run("sagemaker", "list-notebook-instances", "--region", region); err == nil {
		WriteCache(region+":sagemaker-notebooks", data)
		results = append(results, SyncResult{Service: "sagemaker-notebooks", Count: countKey(data, "NotebookInstances")})
	} else {
		results = append(results, SyncResult{Service: "sagemaker-notebooks", Error: err.Error()})
	}
	step("sagemaker notebooks")

	// SageMaker Endpoints
	if data, err := awscli.Run("sagemaker", "list-endpoints", "--region", region); err == nil {
		WriteCache(region+":sagemaker-endpoints", data)
		results = append(results, SyncResult{Service: "sagemaker-endpoints", Count: countKey(data, "Endpoints")})
	} else {
		results = append(results, SyncResult{Service: "sagemaker-endpoints", Error: err.Error()})
	}
	step("sagemaker endpoints")

	// SageMaker Models
	if data, err := awscli.Run("sagemaker", "list-models", "--region", region); err == nil {
		WriteCache(region+":sagemaker-models", data)
		results = append(results, SyncResult{Service: "sagemaker-models", Count: countKey(data, "Models")})
	} else {
		results = append(results, SyncResult{Service: "sagemaker-models", Error: err.Error()})
	}
	step("sagemaker models")

	// Bedrock Foundation Models
	if data, err := awscli.Run("bedrock", "list-foundation-models", "--region", region); err == nil {
		WriteCache(region+":bedrock-models", data)
		results = append(results, SyncResult{Service: "bedrock-models", Count: countKey(data, "modelSummaries")})
	} else {
		results = append(results, SyncResult{Service: "bedrock-models", Error: err.Error()})
	}
	step("bedrock models")

	// Bedrock Custom Models
	if data, err := awscli.Run("bedrock", "list-custom-models", "--region", region); err == nil {
		WriteCache(region+":bedrock-custom", data)
		results = append(results, SyncResult{Service: "bedrock-custom", Count: countKey(data, "modelSummaries")})
	} else {
		results = append(results, SyncResult{Service: "bedrock-custom", Error: err.Error()})
	}
	step("bedrock custom models")

	return results, nil
}

func LoadAIData(region string) (*AIData, error) {
	data := &AIData{}

	// SageMaker Notebooks
	if raw, err := ReadCache(region + ":sagemaker-notebooks"); err == nil && raw != nil {
		var resp struct {
			NotebookInstances []json.RawMessage `json:"NotebookInstances"`
		}
		json.Unmarshal(raw, &resp)
		for _, nb := range resp.NotebookInstances {
			data.SageMakerNotebooks = append(data.SageMakerNotebooks, parseSageMakerNotebook(nb))
		}
	}

	// SageMaker Endpoints
	if raw, err := ReadCache(region + ":sagemaker-endpoints"); err == nil && raw != nil {
		var resp struct {
			Endpoints []json.RawMessage `json:"Endpoints"`
		}
		json.Unmarshal(raw, &resp)
		for _, ep := range resp.Endpoints {
			data.SageMakerEndpoints = append(data.SageMakerEndpoints, parseSageMakerEndpoint(ep, region))
		}
	}

	// SageMaker Models
	if raw, err := ReadCache(region + ":sagemaker-models"); err == nil && raw != nil {
		var resp struct {
			Models []json.RawMessage `json:"Models"`
		}
		json.Unmarshal(raw, &resp)
		for _, m := range resp.Models {
			data.SageMakerModels = append(data.SageMakerModels, parseSageMakerModel(m))
		}
	}

	// Bedrock Foundation Models
	if raw, err := ReadCache(region + ":bedrock-models"); err == nil && raw != nil {
		var resp struct {
			ModelSummaries []json.RawMessage `json:"modelSummaries"`
		}
		json.Unmarshal(raw, &resp)
		for _, m := range resp.ModelSummaries {
			data.BedrockModels = append(data.BedrockModels, parseBedrockModel(m))
		}
	}

	// Bedrock Custom Models
	if raw, err := ReadCache(region + ":bedrock-custom"); err == nil && raw != nil {
		var resp struct {
			ModelSummaries []json.RawMessage `json:"modelSummaries"`
		}
		json.Unmarshal(raw, &resp)
		for _, m := range resp.ModelSummaries {
			data.BedrockCustom = append(data.BedrockCustom, parseBedrockCustomModel(m))
		}
	}

	return data, nil
}

func parseSageMakerNotebook(raw json.RawMessage) SageMakerNotebook {
	var nb struct {
		NotebookInstanceName string   `json:"NotebookInstanceName"`
		NotebookInstanceStatus string `json:"NotebookInstanceStatus"`
		InstanceType         string   `json:"InstanceType"`
		CreationTime         string   `json:"CreationTime"`
		Url                  string   `json:"Url"`
		DirectInternetAccess string   `json:"DirectInternetAccess"`
		SubnetId             string   `json:"SubnetId"`
		SecurityGroups       []string `json:"SecurityGroups"`
		RoleArn              string   `json:"RoleArn"`
		VolumeSizeInGB       int      `json:"VolumeSizeInGB"`
	}
	json.Unmarshal(raw, &nb)

	created := nb.CreationTime
	if t, err := time.Parse(time.RFC3339Nano, nb.CreationTime); err == nil {
		created = t.Format("2006-01-02 15:04")
	}

	roleName := extractRoleName(nb.RoleArn)

	return SageMakerNotebook{
		Name:             nb.NotebookInstanceName,
		Status:           nb.NotebookInstanceStatus,
		InstanceType:     nb.InstanceType,
		CreationTime:     created,
		Url:              nb.Url,
		DirectInternetAccess: nb.DirectInternetAccess,
		SubnetId:         nb.SubnetId,
		SecurityGroups:   nb.SecurityGroups,
		RoleArn:          nb.RoleArn,
		RoleName:         roleName,
		VolumeSizeGB:     nb.VolumeSizeInGB,
	}
}

func parseSageMakerEndpoint(raw json.RawMessage, region string) SageMakerEndpoint {
	var ep struct {
		EndpointName   string `json:"EndpointName"`
		EndpointStatus string `json:"EndpointStatus"`
		CreationTime   string `json:"CreationTime"`
	}
	json.Unmarshal(raw, &ep)

	created := ep.CreationTime
	if t, err := time.Parse(time.RFC3339Nano, ep.CreationTime); err == nil {
		created = t.Format("2006-01-02 15:04")
	}

	endpoint := SageMakerEndpoint{
		Name:         ep.EndpointName,
		Status:       ep.EndpointStatus,
		CreationTime: created,
	}

	// Get endpoint config for model and instance details
	if descData, err := awscli.Run("sagemaker", "describe-endpoint",
		"--endpoint-name", ep.EndpointName, "--region", region); err == nil {
		var desc struct {
			EndpointConfigName string `json:"EndpointConfigName"`
		}
		json.Unmarshal(descData, &desc)

		if desc.EndpointConfigName != "" {
			if cfgData, err := awscli.Run("sagemaker", "describe-endpoint-config",
				"--endpoint-config-name", desc.EndpointConfigName, "--region", region); err == nil {
				var cfg struct {
					ProductionVariants []struct {
						ModelName            string `json:"ModelName"`
						InstanceType         string `json:"InstanceType"`
						InitialInstanceCount int    `json:"InitialInstanceCount"`
					} `json:"ProductionVariants"`
				}
				json.Unmarshal(cfgData, &cfg)
				if len(cfg.ProductionVariants) > 0 {
					endpoint.ModelName = cfg.ProductionVariants[0].ModelName
					endpoint.InstanceType = cfg.ProductionVariants[0].InstanceType
					endpoint.InstanceCount = cfg.ProductionVariants[0].InitialInstanceCount
				}
			}
		}
	}

	return endpoint
}

func parseSageMakerModel(raw json.RawMessage) SageMakerModel {
	var m struct {
		ModelName    string `json:"ModelName"`
		CreationTime string `json:"CreationTime"`
		ModelArn     string `json:"ModelArn"`
	}
	json.Unmarshal(raw, &m)

	created := m.CreationTime
	if t, err := time.Parse(time.RFC3339Nano, m.CreationTime); err == nil {
		created = t.Format("2006-01-02 15:04")
	}

	return SageMakerModel{
		Name:         m.ModelName,
		CreationTime: created,
	}
}

func parseBedrockModel(raw json.RawMessage) BedrockModel {
	var m struct {
		ModelId              string   `json:"modelId"`
		ModelName            string   `json:"modelName"`
		ProviderName         string   `json:"providerName"`
		InputModalities      []string `json:"inputModalities"`
		OutputModalities     []string `json:"outputModalities"`
		ResponseStreamingSupported bool `json:"responseStreamingSupported"`
	}
	json.Unmarshal(raw, &m)

	return BedrockModel{
		ModelId:     m.ModelId,
		ModelName:   m.ModelName,
		Provider:    m.ProviderName,
		InputModes:  m.InputModalities,
		OutputModes: m.OutputModalities,
		Streaming:   m.ResponseStreamingSupported,
	}
}

func parseBedrockCustomModel(raw json.RawMessage) BedrockCustomModel {
	var m struct {
		ModelName    string `json:"modelName"`
		ModelArn     string `json:"modelArn"`
		BaseModelId  string `json:"baseModelIdentifier"`
		CreationTime string `json:"creationTime"`
	}
	json.Unmarshal(raw, &m)

	created := m.CreationTime
	if t, err := time.Parse(time.RFC3339Nano, m.CreationTime); err == nil {
		created = t.Format("2006-01-02 15:04")
	}

	return BedrockCustomModel{
		ModelName:    m.ModelName,
		ModelArn:     m.ModelArn,
		BaseModelId:  m.BaseModelId,
		CreationTime: created,
	}
}

func extractRoleName(arn string) string {
	// arn:aws:iam::123456789012:role/SageMakerRole → SageMakerRole
	parts := strings.Split(arn, "/")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	return ""
}
