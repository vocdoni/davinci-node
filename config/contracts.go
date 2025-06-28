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
		ProcessRegistrySmartContract:      "0xA321c29Eb8a614800Ff737D16F160054Fb5B39d7",
		OrganizationRegistrySmartContract: "0x222c787f2d3Ae83Ff488d0334118cBBC528df1A3",
		ResultsZKVerifier:                 "0x64c5BAF50262B071aF82f82B6e5FDE83e377D4Ae",
		StateTransitionZKVerifier:         "0x8b31a0a00727B6dbcc1223487B688490fb624ff4",
	},
	"uzh": {
		ProcessRegistrySmartContract:      "0x69B16f67Bd2fB18bD720379E9C1Ef5EaD3872d67",
		OrganizationRegistrySmartContract: "0xf7BCE4546805547bE526Ca864d6722Ed193E51Aa",
		ResultsZKVerifier:                 "0x00c7F87731346F592197E49A90Ad6EC236Ad9985",
		StateTransitionZKVerifier:         "0x5e4673CD378F05cc3Ae25804539c91E711548741",
	},
}

// AvailableNetworks contains the list of networks where Davinci is deployed.
var AvailableNetworks = map[string]uint32{
	"sep":  11155111,
	"uzh":  710,
	"test": 1337, // Local test network
}
