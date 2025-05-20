package config

// DavinciWeb3Config contains the smart contract addresses for Davinci.
type DavinciWeb3Config struct {
	ProcessRegistrySmartContract      string
	OrganizationRegistrySmartContract string
	ResultsSmartContract              string
}

// DefaultConfig contains the default smart contract addresses for Davinci by network.
var DefaultConfig = map[string]DavinciWeb3Config{
	"sep": {
		ProcessRegistrySmartContract:      "0x64bA009A7955c06a9Fde21a70044fE257c141779",
		OrganizationRegistrySmartContract: "0xDdf46896BD0909F0b565e2aD5cdD43F975aE933E",
		ResultsSmartContract:              "0xDdf46896BD0909F0b565e2aD5cdD43F975aE933E", // duplicate for now
	},
}

// AvailableNetworks contains the list of networks where Davinci is deployed.
var AvailableNetworks = []string{
	"sep",
}
