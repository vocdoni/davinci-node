package config

// DavinciWeb3Config contains the smart contract addresses for Davinci.
type DavinciWeb3Config struct {
	ProcessRegistrySmartContract      string
	OrganizationRegistrySmartContract string
	ResultsZKVerifier                 string
	StateTransitionZKVerifier         string
}

// DefaultConfig contains the default smart contract addresses for Davinci by network.
var DefaultConfig = map[string]DavinciWeb3Config{
	"sep": {
		ProcessRegistrySmartContract:      "0xB538d3fBF4C9cF3A31d6c6E2d15B405Ff66c3B15",
		OrganizationRegistrySmartContract: "0x159630c26381AB98867E4A3f631fB69fE6b48DBF",
		ResultsZKVerifier:                 "0xE2046392a389795Fe6F2b15C8A8Fc1582554828E", // duplicate for now
		StateTransitionZKVerifier:         "0x13249EF15BEa50736b46BF8fF4d2DC2b4B32F151",
	},
}

// AvailableNetworks contains the list of networks where Davinci is deployed.
var AvailableNetworks = []string{
	"sep",
}
