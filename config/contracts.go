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
		ProcessRegistrySmartContract:      "0x178C9E0bceEa6535e13E370F1B3F69b881A2F554",
		OrganizationRegistrySmartContract: "0x6f9e376fe682B7B21AAB9c62C5d2dc55B88Dd3C4",
		ResultsZKVerifier:                 "0x6084fe520e7c6616be1C75713F63c5bBc31046FE",
		StateTransitionZKVerifier:         "0x40EFFAC7c37Dfc162F92CBcE4E16ACbF8D695A72",
	},
	"uzh": {
		ProcessRegistrySmartContract:      "0xBC1A75100023add2E798f16790704372E2a36085",
		OrganizationRegistrySmartContract: "0x4102a669FAAD42e6202b2c7bF5d6C5aB0F722217",
		ResultsZKVerifier:                 "0xf6Fb4C1348dEBf52f7D5Fa59B920Ac403dbc4627",
		StateTransitionZKVerifier:         "0xbae94CAFe76bAEcA335a8a338483957BB1E85Eb5",
	},
}

// AvailableNetworks contains the list of networks where Davinci is deployed.
var AvailableNetworks = []string{
	"sep",
	"uzh",
}
