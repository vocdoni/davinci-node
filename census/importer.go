package census

import (
	"context"
	"fmt"

	"github.com/vocdoni/census3-bigquery/censusdb"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

// ImporterPlugin defines the interface for census import plugins. Each plugin
// must implement the following methods:
//   - ValidURI: checks if the provided targetURI is valid for this plugin.
//   - DownloadAndImportCensus: downloads and imports the census from the
//     specified targetURI into the provided censusDB, verifying against the
//     expectedRoot.
type ImporterPlugin interface {
	ValidURI(targetURI string) bool
	DownloadAndImportCensus(ctx context.Context, censusDB *censusdb.CensusDB, targetURI string, expectedRoot types.HexBytes) error
}

// CensusImporter is responsible for importing censuses from various origins.
type CensusImporter struct {
	storage *storage.Storage
	plugins []ImporterPlugin
}

// NewCensusImporter creates a new CensusImporter with the given storage and
// optional plugins. If no plugins are provided, the importer will not be able
// to import any censuses. The caller is responsible for providing the desired
// plugins in the correct order of precedence.
func NewCensusImporter(stg *storage.Storage, plugins ...ImporterPlugin) *CensusImporter {
	return &CensusImporter{
		storage: stg,
		plugins: plugins,
	}
}

// ImportCensus downloads and imports the census from the given URI. It checks
// the census origin to ensure that it is supported. Merkle Tree censuses are
// downloaded and imported using the appropriate plugin based on the URI. CSP
// censuses do not require downloading, as the census data is managed by the
// CSP itself. Other census origins are not supported. It returns an error if
// the download or import fails.
func (d *CensusImporter) ImportCensus(ctx context.Context, census *types.Census) error {
	if census == nil {
		return fmt.Errorf("census is nil")
	}
	if !census.CensusOrigin.Valid() {
		return fmt.Errorf("invalid census origin: %s", census.CensusOrigin.String())
	}
	switch {
	case census.CensusOrigin.IsMerkleTree():
		for _, plugin := range d.plugins {
			if plugin.ValidURI(census.CensusURI) {
				return plugin.DownloadAndImportCensus(
					ctx,
					d.storage.CensusDB(),
					census.CensusURI,
					census.CensusRoot,
				)
			}
		}
		return fmt.Errorf("no importer plugin found for census URI: %s", census.CensusURI)
	case census.CensusOrigin.IsCSP():
		// CSP-based census importers do not require downloading, as the
		// census data is managed by the CSP itself.
		return nil
	default:
		return fmt.Errorf("unsupported census origin: %s", census.CensusOrigin.String())
	}
}
