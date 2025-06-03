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
		ProcessRegistrySmartContract:      "0xd721ACDc64758F8eCb0ae016C7aE563509D9Ac6F",
		OrganizationRegistrySmartContract: "0x9F354dce18279F6bC3b17A5df78CbD3536e3f18c",
		ResultsZKVerifier:                 "0xEE95EbD00BFA339BcCE28011Ee7e4D0377C22dDB",
		StateTransitionZKVerifier:         "0xdD1e78b5Ec6ADB7D3C9243FE42fD9180e9Cc8F43",
	},
}

// AvailableNetworks contains the list of networks where Davinci is deployed.
var AvailableNetworks = []string{
	"sep",
}
