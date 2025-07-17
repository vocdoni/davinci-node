package config

import _ "embed"

// KZGTrustedSetup contains the embedded KZG trusted setup data
//
//go:embed kzg_trusted_setup.txt
var KZGTrustedSetup []byte
