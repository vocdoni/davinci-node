package metadata

import "github.com/vocdoni/davinci-node/types"

func testMetadata() *types.Metadata {
	return &types.Metadata{
		Title: types.MultilingualString{
			"en": "Davinci metadata",
			"es": "Metadatos Davinci",
		},
		Description: types.MultilingualString{
			"en": "Metadata fixture used in package tests",
		},
		Media: types.MediaMetadata{
			Header: "https://example.org/header.png",
			Logo:   "https://example.org/logo.png",
		},
		Questions: []types.Question{
			{
				Title: types.MultilingualString{
					"en": "Choose one",
				},
				Description: types.MultilingualString{
					"en": "Pick the best option",
				},
				Choices: []types.Choice{
					{
						Title: types.MultilingualString{
							"en": "Option A",
						},
						Value: 1,
						Meta: types.GenericMetadata{
							"color": "blue",
						},
					},
					{
						Title: types.MultilingualString{
							"en": "Option B",
						},
						Value: 2,
						Meta: types.GenericMetadata{
							"color": "green",
						},
					},
				},
				Meta: types.GenericMetadata{
					"nested": types.GenericMetadata{
						"enabled": true,
					},
				},
			},
		},
		Type: types.ProcessType{
			Name: "survey",
			Properties: types.GenericMetadata{
				"strategy": "single-choice",
			},
		},
		Version: "1.0.0",
		Meta: types.GenericMetadata{
			"active": true,
			"count":  float64(2),
			"nested": types.GenericMetadata{
				"source": "tests",
			},
		},
	}
}

func testUnsupportedMetadata() *types.Metadata {
	metadata := testMetadata()
	metadata.Meta["invalid"] = func() {}
	return metadata
}
