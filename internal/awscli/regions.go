package awscli

var RegionNames = map[string]string{
	"us-east-1":      "N. Virginia",
	"us-east-2":      "Ohio",
	"us-west-1":      "N. California",
	"us-west-2":      "Oregon",
	"af-south-1":     "Cape Town",
	"ap-east-1":      "Hong Kong",
	"ap-south-1":     "Mumbai",
	"ap-south-2":     "Hyderabad",
	"ap-southeast-1": "Singapore",
	"ap-southeast-2": "Sydney",
	"ap-southeast-3": "Jakarta",
	"ap-southeast-4": "Melbourne",
	"ap-southeast-5": "Malaysia",
	"ap-northeast-1": "Tokyo",
	"ap-northeast-2": "Seoul",
	"ap-northeast-3": "Osaka",
	"ca-central-1":   "Canada",
	"ca-west-1":      "Calgary",
	"eu-central-1":   "Frankfurt",
	"eu-central-2":   "Zurich",
	"eu-west-1":      "Ireland",
	"eu-west-2":      "London",
	"eu-west-3":      "Paris",
	"eu-south-1":     "Milan",
	"eu-south-2":     "Spain",
	"eu-north-1":     "Stockholm",
	"il-central-1":   "Tel Aviv",
	"me-south-1":     "Bahrain",
	"me-central-1":   "UAE",
	"sa-east-1":      "Sao Paulo",
	"mx-central-1":   "Mexico City",
}

func RegionDisplayName(code string) string {
	if name, ok := RegionNames[code]; ok {
		return code + " (" + name + ")"
	}
	return code
}
