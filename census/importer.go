package census

import (
	"context"
	"fmt"

	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

// CensusImporter is responsible for importing censuses from various origins.
type CensusImporter struct {
	storage *storage.Storage
}

// NewCensusImporter creates a new CensusImporter with the given storage.
func NewCensusImporter(stg *storage.Storage) *CensusImporter {
	return &CensusImporter{
		storage: stg,
	}
}

// ImportCensus downloads and imports the census from the given URI based on
// its origin:
//   - For CensusOriginMerkleTree, it expects a URL pointing to a JSON dump of
//     the census merkle tree, downloads it, and imports it into the census DB
//     by its census root.
//
// It returns an error if the download or import fails.
func (d *CensusImporter) ImportCensus(ctx context.Context, census *types.Census) error {
	log.Debugw("downloading census",
		"origin", census.CensusOrigin.String(),
		"uri", census.CensusURI)

	switch census.CensusOrigin {
	case types.CensusOriginMerkleTreeOffchainStaticV1:
		// Use JSON dump importer for Merkle Tree censuses
		if err := downloadAndImportJSON(
			d.storage,
			census.CensusURI,
			census.CensusRoot,
		); err != nil {
			return fmt.Errorf("failed to import census from JSON dump: %w", err)
		}
	default:
		return fmt.Errorf("unsupported census origin: %s", census.CensusOrigin.String())
	}
	log.Infow("census imported",
		"origin", census.CensusOrigin.String(),
		"root", census.CensusRoot.String())
	return nil
}
