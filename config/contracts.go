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
		ProcessRegistrySmartContract:      "0xe3E73E831a059203d41Ae27D6D39d62726775379",
		OrganizationRegistrySmartContract: "0x78025f376d662b9C21a5f9465b091763048bCcCC",
		ResultsZKVerifier:                 "0x9eAC754B0848F5549AE6d3740e1A6202f19BE8A6",
		StateTransitionZKVerifier:         "0x81C58e330E35Ba5D54439ECaC59c6FE503F05Fc7",
	},
	"uzh": {
		ProcessRegistrySmartContract:      "0x650937867b8c9D7261DEAD8d94eb47cf15A80Ded",
		OrganizationRegistrySmartContract: "0x49E2E261a734454f07b27D1A6978FFAE44618D03",
		ResultsZKVerifier:                 "0xfc14B2Bbcee53d74362416b1031dE78c667016b6",
		StateTransitionZKVerifier:         "0xAaE00882AD969543c308Ed60c3145db7264A02f4",
	},
}

// AvailableNetworks contains the list of networks where Davinci is deployed.
var AvailableNetworks = map[string]uint32{
	"sep":  11155111,
	"uzh":  710,
	"test": 1337, // Local test network
}
