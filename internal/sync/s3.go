package sync

import (
	"encoding/json"
	"time"

	"github.com/estrados/simply-aws/internal/awscli"
)

type S3Data struct {
	Buckets []S3Bucket `json:"buckets"`
}

type S3Bucket struct {
	Name              string          `json:"Name"`
	CreationDate      string          `json:"CreationDate"`
	Region            string          `json:"Region"`
	Access            string          `json:"Access"`            // "private", "public", "unknown"
	Versioning        string          `json:"Versioning"`        // "Enabled", "Suspended", "Disabled"
	PublicAccessBlock *S3PublicBlock  `json:"PublicAccessBlock"`
	PolicyPublic      bool            `json:"PolicyPublic"`
	ACLPublic         bool            `json:"ACLPublic"`
}

type S3PublicBlock struct {
	BlockPublicAcls       bool `json:"BlockPublicAcls"`
	IgnorePublicAcls      bool `json:"IgnorePublicAcls"`
	BlockPublicPolicy     bool `json:"BlockPublicPolicy"`
	RestrictPublicBuckets bool `json:"RestrictPublicBuckets"`
}

func LoadS3Data() (*S3Data, error) {
	data := &S3Data{}

	raw, err := ReadCache("s3")
	if err != nil || raw == nil {
		return data, err
	}

	var resp struct {
		Buckets []json.RawMessage `json:"Buckets"`
	}
	json.Unmarshal(raw, &resp)

	for _, b := range resp.Buckets {
		data.Buckets = append(data.Buckets, parseS3Bucket(b))
	}

	return data, nil
}

func parseS3Bucket(raw json.RawMessage) S3Bucket {
	var b struct {
		Name         string `json:"Name"`
		CreationDate string `json:"CreationDate"`
	}
	json.Unmarshal(raw, &b)

	created := b.CreationDate
	if t, err := time.Parse(time.RFC3339, b.CreationDate); err == nil {
		created = t.Format("2006-01-02 15:04")
	}

	return S3Bucket{
		Name:         b.Name,
		CreationDate: created,
		Access:       "unknown",
		Versioning:   "Unknown",
	}
}

// SyncS3WithRegions syncs bucket list then fetches per-bucket details.
func SyncS3WithRegions() (*SyncResult, error) {
	result, err := syncS3()
	if err != nil {
		return nil, err
	}

	s3Data, _ := LoadS3Data()
	for i, bucket := range s3Data.Buckets {
		// Region
		if regionData, err := awscli.Run("s3api", "get-bucket-location", "--bucket", bucket.Name); err == nil {
			var loc struct {
				LocationConstraint *string `json:"LocationConstraint"`
			}
			json.Unmarshal(regionData, &loc)
			if loc.LocationConstraint == nil || *loc.LocationConstraint == "" {
				s3Data.Buckets[i].Region = "us-east-1"
			} else {
				s3Data.Buckets[i].Region = *loc.LocationConstraint
			}
		}

		// Public Access Block
		if pabData, err := awscli.Run("s3api", "get-public-access-block", "--bucket", bucket.Name); err == nil {
			var pab struct {
				PublicAccessBlockConfiguration S3PublicBlock `json:"PublicAccessBlockConfiguration"`
			}
			json.Unmarshal(pabData, &pab)
			s3Data.Buckets[i].PublicAccessBlock = &pab.PublicAccessBlockConfiguration
		}

		// Policy status (is policy public?)
		if polData, err := awscli.Run("s3api", "get-bucket-policy-status", "--bucket", bucket.Name); err == nil {
			var pol struct {
				PolicyStatus struct {
					IsPublic bool `json:"IsPublic"`
				} `json:"PolicyStatus"`
			}
			json.Unmarshal(polData, &pol)
			s3Data.Buckets[i].PolicyPublic = pol.PolicyStatus.IsPublic
		}

		// ACL check
		if aclData, err := awscli.Run("s3api", "get-bucket-acl", "--bucket", bucket.Name); err == nil {
			var acl struct {
				Grants []struct {
					Grantee struct {
						URI string `json:"URI"`
					} `json:"Grantee"`
				} `json:"Grants"`
			}
			json.Unmarshal(aclData, &acl)
			for _, g := range acl.Grants {
				if g.Grantee.URI == "http://acs.amazonaws.com/groups/global/AllUsers" ||
					g.Grantee.URI == "http://acs.amazonaws.com/groups/global/AuthenticatedUsers" {
					s3Data.Buckets[i].ACLPublic = true
					break
				}
			}
		}

		// Versioning
		if verData, err := awscli.Run("s3api", "get-bucket-versioning", "--bucket", bucket.Name); err == nil {
			var ver struct {
				Status string `json:"Status"`
			}
			json.Unmarshal(verData, &ver)
			if ver.Status == "" {
				s3Data.Buckets[i].Versioning = "Disabled"
			} else {
				s3Data.Buckets[i].Versioning = ver.Status
			}
		}

		// Determine overall access
		s3Data.Buckets[i].Access = determineAccess(s3Data.Buckets[i])
	}

	enriched, _ := json.Marshal(s3Data)
	WriteCache("s3:enriched", enriched)

	return result, nil
}

func determineAccess(b S3Bucket) string {
	// If all public access blocks are on → definitely private
	if b.PublicAccessBlock != nil {
		pab := b.PublicAccessBlock
		if pab.BlockPublicAcls && pab.IgnorePublicAcls && pab.BlockPublicPolicy && pab.RestrictPublicBuckets {
			return "private"
		}
	}

	// If policy or ACL is public → public
	if b.PolicyPublic || b.ACLPublic {
		return "public"
	}

	// Has public access block but not all enabled
	if b.PublicAccessBlock != nil {
		return "private"
	}

	return "unknown"
}

func LoadS3DataEnriched() (*S3Data, error) {
	raw, err := ReadCache("s3:enriched")
	if err != nil || raw == nil {
		return LoadS3Data()
	}
	var data S3Data
	json.Unmarshal(raw, &data)
	if len(data.Buckets) == 0 {
		return LoadS3Data()
	}
	return &data, nil
}
