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
		ProcessRegistrySmartContract:      "0x6C9d0f85e11970ab4D354ec4fbDe0e34deA8Da7f",
		OrganizationRegistrySmartContract: "0x18F0cB7B6dbDcD54f2dAB08232e660A69C4b0f63",
		ResultsSmartContract:              "0x18F0cB7B6dbDcD54f2dAB08232e660A69C4b0f63", // duplicate for now
	},
}

// AvailableNetworks contains the list of networks where Davinci is deployed.
var AvailableNetworks = []string{
	"sep",
}
