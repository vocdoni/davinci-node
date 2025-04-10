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
		ProcessRegistrySmartContract:      "0xd512481d0Fa6d975f9B186a9f6e59ea8E12D2C2b",
		OrganizationRegistrySmartContract: "0x3d0b39c0239329955b9F0E8791dF9Aa84133c861",
		ResultsSmartContract:              "0x3d0b39c0239329955b9F0E8791dF9Aa84133c861", // duplicate for now
	},
}

// AvailableNetworks contains the list of networks where Davinci is deployed.
var AvailableNetworks = []string{
	"sep",
}
