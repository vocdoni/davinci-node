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
		ProcessRegistrySmartContract:      "0x40939Ec9FD872eb79A1723B559572dfD71a05d11",
		OrganizationRegistrySmartContract: "0xe7136ED5a7b0e995A8fe35d8B1B815E4160cB491",
		ResultsZKVerifier:                 "0x1188cEbB56ecc90e2bAe5c914274C81Fe1a22e67",
		StateTransitionZKVerifier:         "0xb7A142D24b9220eCBC4f7fcB89Ee952a6C7E332a",
	},
	"uzh": {
		ProcessRegistrySmartContract:      "0x69B16f67Bd2fB18bD720379E9C1Ef5EaD3872d67",
		OrganizationRegistrySmartContract: "0xf7BCE4546805547bE526Ca864d6722Ed193E51Aa",
		ResultsZKVerifier:                 "0x00c7F87731346F592197E49A90Ad6EC236Ad9985",
		StateTransitionZKVerifier:         "0x5e4673CD378F05cc3Ae25804539c91E711548741",
	},
	"test": {
		ProcessRegistrySmartContract:      "0xcf7ed3acca5a467e9e704c703e8d87f634fb0fc9",
		OrganizationRegistrySmartContract: "0x5fbdb2315678afecb367f032d93f642f64180aa3",
		ResultsZKVerifier:                 "0x9fe46736679d2d9a65f0992f2272de9f3c7fa6e0",
		StateTransitionZKVerifier:         "0xe7f1725e7734ce288f8367e1bb143e90bb3f0512",
	},
}

// AvailableNetworks contains the list of networks where Davinci is deployed.
var AvailableNetworks = map[string]uint32{
	"sep":  11155111,
	"uzh":  710,
	"test": 1337, // Local test network
}
