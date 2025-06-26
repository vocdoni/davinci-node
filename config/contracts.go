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
		ProcessRegistrySmartContract:      "0xa536520468523BC0732396956BAa644D97B8A342",
		OrganizationRegistrySmartContract: "0x4b8f6A584Beb64EC95ffc9E530E386B4770185bb",
		ResultsZKVerifier:                 "0x3F879C7174F30856EBA6A86E15895eF47477F637",
		StateTransitionZKVerifier:         "0x9e9aB32412c860ED67F5BAEeF6f75dd1FabBcb34",
	},
	"uzh": {
		ProcessRegistrySmartContract:      "0x04e823c0b2021E7976FAC2Ba8F8D9748CB800299",
		OrganizationRegistrySmartContract: "0x08AA2EDF223935B0C5D68FC4E1E70Ab7DD46Ef36",
		ResultsZKVerifier:                 "0x0D244BAC819e2C6E241c64438eeB42df38Be4297",
		StateTransitionZKVerifier:         "0x2aa97d71EFDE7635512AF1be2b75eaB0F4Fa934A",
	},
}

// AvailableNetworks contains the list of networks where Davinci is deployed.
var AvailableNetworks = map[string]uint32{
	"sep":  11155111,
	"uzh":  710,
	"test": 1337, // Local test network
}
