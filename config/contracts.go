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
		ProcessRegistrySmartContract:      "0x7c2Fdd6b411e40d9f02B496D1cA1EA767bC3d337",
		OrganizationRegistrySmartContract: "0x82A6492db3c26E666634FF8EFDac3Fe8dbe5652C",
		ResultsSmartContract:              "0x82A6492db3c26E666634FF8EFDac3Fe8dbe5652C", // duplicate for now
	},
}

// AvailableNetworks contains the list of networks where Davinci is deployed.
var AvailableNetworks = []string{
	"sep",
}
