package metadata

import "github.com/vocdoni/davinci-node/types"

const gatewayPattern = "%s?pinataGatewayToken=%s"

type PinataMetadataProvider struct {
	GatewayUrl  string
	AccessToken string
}

func NewPinataMetadataProvider(gatewayUrl string, accessToken string) *PinataMetadataProvider {
	return &PinataMetadataProvider{
		GatewayUrl:  gatewayUrl,
		AccessToken: accessToken,
	}
}

func (p *PinataMetadataProvider) Metadata(key types.HexBytes) (*types.Metadata, error) {
	return nil, nil
}

func (p *PinataMetadataProvider) SetMetadata(metadata *types.Metadata) (types.HexBytes, error) {
	return nil, nil
}
