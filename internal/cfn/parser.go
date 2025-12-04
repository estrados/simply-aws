package cfn

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Template struct {
	File         string                 `json:"file"`
	AWSVersion   string                 `json:"awsTemplateFormatVersion,omitempty"`
	Description  string                 `json:"description,omitempty"`
	Parameters   map[string]interface{} `json:"parameters,omitempty"`
	Resources    map[string]Resource    `json:"resources,omitempty"`
	Outputs      map[string]interface{} `json:"outputs,omitempty"`
}

type Resource struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties,omitempty"`
}

type rawTemplate struct {
	AWSVersion  string                            `yaml:"AWSTemplateFormatVersion"`
	Description string                            `yaml:"Description"`
	Parameters  map[string]interface{}             `yaml:"Parameters"`
	Resources   map[string]rawResource             `yaml:"Resources"`
	Outputs     map[string]interface{}             `yaml:"Outputs"`
}

type rawResource struct {
	Type       string                 `yaml:"Type"`
	Properties map[string]interface{} `yaml:"Properties"`
}

func ParseFile(path string) (*Template, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data, path)
}

func Parse(data []byte, filename string) (*Template, error) {
	var raw rawTemplate
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	t := &Template{
		File:        filename,
		AWSVersion:  raw.AWSVersion,
		Description: raw.Description,
		Parameters:  raw.Parameters,
		Outputs:     raw.Outputs,
		Resources:   make(map[string]Resource),
	}

	for name, r := range raw.Resources {
		t.Resources[name] = Resource{
			Type:       r.Type,
			Properties: r.Properties,
		}
	}

	return t, nil
}
