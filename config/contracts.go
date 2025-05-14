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
		ProcessRegistrySmartContract:      "0xdecC4F656BE4C96617af7EeEaD0042a8855Fee9c",
		OrganizationRegistrySmartContract: "0x218Ca677d701f535A239b1d4a4db2384CE81f371",
		ResultsSmartContract:              "0x218Ca677d701f535A239b1d4a4db2384CE81f371", // duplicate for now
	},
}

// AvailableNetworks contains the list of networks where Davinci is deployed.
var AvailableNetworks = []string{
	"sep",
}
