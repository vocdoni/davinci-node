package config

// DavinciWeb3Config contains the smart contract addresses for Davinci.
type DavinciWeb3Config struct {
	ProcessRegistrySmartContract      string
	OrganizationRegistrySmartContract string
	ResultsSmartContract              string
	StateTransitionZKVerifier         string
}

// DefaultConfig contains the default smart contract addresses for Davinci by network.
var DefaultConfig = map[string]DavinciWeb3Config{
	"sep": {
		ProcessRegistrySmartContract:      "0x449598f6A4C53ABA99e6029f92757f110bFCEdB5",
		OrganizationRegistrySmartContract: "0x799dF2b1AF3393d821b4552a28089282267403Be",
		ResultsSmartContract:              "0x799dF2b1AF3393d821b4552a28089282267403Be", // duplicate for now
		StateTransitionZKVerifier:         "0xb2e927353a99EF22E180BfA5F1BF10F86f124326",
	},
}

// AvailableNetworks contains the list of networks where Davinci is deployed.
var AvailableNetworks = []string{
	"sep",
}
