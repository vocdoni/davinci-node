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
		ProcessRegistrySmartContract:      "0x53871fb5246fbBe47C183466dBa1D92ff0186034",
		OrganizationRegistrySmartContract: "0x72033a7007c769223baC4b48E0eE236f5EEE8252",
		ResultsZKVerifier:                 "0x968f94D8c2AcC52A1Cca47a49bA24FC4737AF2Ac",
		StateTransitionZKVerifier:         "0x2BCb359b27B53656D7960Da0FFBedCcF96Cc6b89",
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
