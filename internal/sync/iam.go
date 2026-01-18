package sync

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/estrados/simply-aws/internal/awscli"
)

type IAMData struct {
	Roles  []IAMRole  `json:"roles"`
	Groups []IAMGroup `json:"groups"`
}

type IAMRole struct {
	RoleName         string           `json:"RoleName"`
	RoleId           string           `json:"RoleId"`
	Arn              string           `json:"Arn"`
	CreateDate       string           `json:"CreateDate"`
	Description      string           `json:"Description"`
	TrustPolicy      []ResourcePolicy `json:"TrustPolicy"`
	AttachedPolicies []string         `json:"AttachedPolicies"`
	InlinePolicies   []string         `json:"InlinePolicies"`
	IsServiceLinked  bool             `json:"IsServiceLinked"`
}

type IAMGroup struct {
	GroupName        string   `json:"GroupName"`
	GroupId          string   `json:"GroupId"`
	Arn              string   `json:"Arn"`
	CreateDate       string   `json:"CreateDate"`
	AttachedPolicies []string `json:"AttachedPolicies"`
	InlinePolicies   []string `json:"InlinePolicies"`
	Members          []string `json:"Members"`
}

func SyncIAMData() ([]SyncResult, error) {
	var results []SyncResult
	data := &IAMData{}

	// Sync roles
	if raw, err := awscli.Run("iam", "list-roles"); err == nil {
		WriteCache("iam:roles", raw)
		var resp struct {
			Roles []struct {
				RoleName                 string          `json:"RoleName"`
				RoleId                   string          `json:"RoleId"`
				Arn                      string          `json:"Arn"`
				CreateDate               string          `json:"CreateDate"`
				Description              string          `json:"Description"`
				AssumeRolePolicyDocument json.RawMessage `json:"AssumeRolePolicyDocument"`
				Path                     string          `json:"Path"`
			} `json:"Roles"`
		}
		json.Unmarshal(raw, &resp)

		for _, r := range resp.Roles {
			role := IAMRole{
				RoleName:        r.RoleName,
				RoleId:          r.RoleId,
				Arn:             r.Arn,
				CreateDate:      formatIAMDate(r.CreateDate),
				Description:     r.Description,
				IsServiceLinked: strings.HasPrefix(r.Path, "/aws-service-role/"),
			}

			// Trust policy is inline in the list-roles response
			if len(r.AssumeRolePolicyDocument) > 0 {
				policyStr := string(r.AssumeRolePolicyDocument)
				// If it's a JSON string (quoted), unquote it
				var unquoted string
				if err := json.Unmarshal(r.AssumeRolePolicyDocument, &unquoted); err == nil {
					policyStr = unquoted
				}
				role.TrustPolicy = ParseResourcePolicies(policyStr)
			}

			// Attached policies
			if polData, err := awscli.Run("iam", "list-attached-role-policies", "--role-name", r.RoleName); err == nil {
				var polResp struct {
					AttachedPolicies []struct {
						PolicyName string `json:"PolicyName"`
					} `json:"AttachedPolicies"`
				}
				json.Unmarshal(polData, &polResp)
				for _, p := range polResp.AttachedPolicies {
					role.AttachedPolicies = append(role.AttachedPolicies, p.PolicyName)
				}
			}

			// Inline policies
			if polData, err := awscli.Run("iam", "list-role-policies", "--role-name", r.RoleName); err == nil {
				var polResp struct {
					PolicyNames []string `json:"PolicyNames"`
				}
				json.Unmarshal(polData, &polResp)
				role.InlinePolicies = polResp.PolicyNames
			}

			data.Roles = append(data.Roles, role)
		}
		results = append(results, SyncResult{Service: "iam-roles", Count: len(resp.Roles)})
	} else {
		results = append(results, SyncResult{Service: "iam-roles", Error: err.Error()})
	}

	// Sync groups
	if raw, err := awscli.Run("iam", "list-groups"); err == nil {
		WriteCache("iam:groups", raw)
		var resp struct {
			Groups []struct {
				GroupName  string `json:"GroupName"`
				GroupId    string `json:"GroupId"`
				Arn        string `json:"Arn"`
				CreateDate string `json:"CreateDate"`
			} `json:"Groups"`
		}
		json.Unmarshal(raw, &resp)

		for _, g := range resp.Groups {
			group := IAMGroup{
				GroupName:  g.GroupName,
				GroupId:    g.GroupId,
				Arn:        g.Arn,
				CreateDate: formatIAMDate(g.CreateDate),
			}

			// Attached policies
			if polData, err := awscli.Run("iam", "list-attached-group-policies", "--group-name", g.GroupName); err == nil {
				var polResp struct {
					AttachedPolicies []struct {
						PolicyName string `json:"PolicyName"`
					} `json:"AttachedPolicies"`
				}
				json.Unmarshal(polData, &polResp)
				for _, p := range polResp.AttachedPolicies {
					group.AttachedPolicies = append(group.AttachedPolicies, p.PolicyName)
				}
			}

			// Inline policies
			if polData, err := awscli.Run("iam", "list-group-policies", "--group-name", g.GroupName); err == nil {
				var polResp struct {
					PolicyNames []string `json:"PolicyNames"`
				}
				json.Unmarshal(polData, &polResp)
				group.InlinePolicies = polResp.PolicyNames
			}

			// Members
			if memData, err := awscli.Run("iam", "get-group", "--group-name", g.GroupName); err == nil {
				var memResp struct {
					Users []struct {
						UserName string `json:"UserName"`
					} `json:"Users"`
				}
				json.Unmarshal(memData, &memResp)
				for _, u := range memResp.Users {
					group.Members = append(group.Members, u.UserName)
				}
			}

			data.Groups = append(data.Groups, group)
		}
		results = append(results, SyncResult{Service: "iam-groups", Count: len(resp.Groups)})
	} else {
		results = append(results, SyncResult{Service: "iam-groups", Error: err.Error()})
	}

	// Cache enriched data
	enriched, _ := json.Marshal(data)
	WriteCache("iam:enriched", enriched)

	return results, nil
}

func LoadIAMData() (*IAMData, error) {
	raw, err := ReadCache("iam:enriched")
	if err != nil || raw == nil {
		return nil, err
	}
	var data IAMData
	json.Unmarshal(raw, &data)
	return &data, nil
}

func formatIAMDate(s string) string {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Format("2006-01-02 15:04")
	}
	return s
}
