package config

// DavinciWeb3Config contains the smart contract addresses for Davinci.
type DavinciWeb3Config struct {
	ProcessRegistrySmartContract      string
	OrganizationRegistrySmartContract string
	ResultsZKVerifier                 string
	StateTransitionZKVerifier         string
}

var TestConfig = DavinciWeb3Config{
	ProcessRegistrySmartContract:      "0xcf7ed3acca5a467e9e704c703e8d87f634fb0fc9",
	OrganizationRegistrySmartContract: "0x5fbdb2315678afecb367f032d93f642f64180aa3",
	ResultsZKVerifier:                 "0x9fe46736679d2d9a65f0992f2272de9f3c7fa6e0",
	StateTransitionZKVerifier:         "0xe7f1725e7734ce288f8367e1bb143e90bb3f0512",
}
