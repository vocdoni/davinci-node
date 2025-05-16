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
		ProcessRegistrySmartContract:      "0x9936DBd1E225eCC44272B4Cb31e7e29799E3a166",
		OrganizationRegistrySmartContract: "0x99502b97707Cc551448516E3120B0AA0202630Be",
		ResultsSmartContract:              "0x99502b97707Cc551448516E3120B0AA0202630Be", // duplicate for now
	},
}

// AvailableNetworks contains the list of networks where Davinci is deployed.
var AvailableNetworks = []string{
	"sep",
}
