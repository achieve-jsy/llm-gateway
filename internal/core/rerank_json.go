package core

import "encoding/json"

func (r *RerankRequest) UnmarshalJSON(data []byte) error {
	var raw struct {
		Model           string          `json:"model"`
		Provider        string          `json:"provider,omitempty"`
		Query           json.RawMessage `json:"query"`
		Documents       json.RawMessage `json:"documents"`
		TopN            *int            `json:"top_n,omitempty"`
		ReturnDocuments *bool           `json:"return_documents,omitempty"`
		MaxChunksPerDoc *int            `json:"max_chunks_per_doc,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	extraFields, err := extractUnknownJSONFields(data,
		"model",
		"provider",
		"query",
		"documents",
		"top_n",
		"return_documents",
		"max_chunks_per_doc",
	)
	if err != nil {
		return err
	}

	r.Model = raw.Model
	r.Provider = raw.Provider
	r.TopN = raw.TopN
	r.ReturnDocuments = raw.ReturnDocuments
	r.MaxChunksPerDoc = raw.MaxChunksPerDoc
	r.ExtraFields = extraFields

	if len(raw.Query) > 0 && string(raw.Query) != "null" {
		var query any
		if err := json.Unmarshal(raw.Query, &query); err != nil {
			return err
		}
		r.Query = query
	}

	if len(raw.Documents) > 0 && string(raw.Documents) != "null" {
		var docs []any
		if err := json.Unmarshal(raw.Documents, &docs); err != nil {
			return err
		}
		r.Documents = docs
	}
	return nil
}

func (r RerankRequest) MarshalJSON() ([]byte, error) {
	type rerankRequestAlias struct {
		Model           string `json:"model"`
		Provider        string `json:"provider,omitempty"`
		Query           any    `json:"query"`
		Documents       []any  `json:"documents"`
		TopN            *int   `json:"top_n,omitempty"`
		ReturnDocuments *bool  `json:"return_documents,omitempty"`
		MaxChunksPerDoc *int   `json:"max_chunks_per_doc,omitempty"`
	}

	return marshalWithUnknownJSONFields(rerankRequestAlias{
		Model:           r.Model,
		Provider:        r.Provider,
		Query:           r.Query,
		Documents:       r.Documents,
		TopN:            r.TopN,
		ReturnDocuments: r.ReturnDocuments,
		MaxChunksPerDoc: r.MaxChunksPerDoc,
	}, r.ExtraFields)
}

func (r *RerankResponse) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID       string         `json:"id"`
		Model    string         `json:"model"`
		Provider string         `json:"provider"`
		Results  []RerankResult `json:"results"`
		Usage    RerankUsage    `json:"usage"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	extraFields, err := extractUnknownJSONFields(data,
		"id",
		"model",
		"provider",
		"results",
		"usage",
	)
	if err != nil {
		return err
	}

	r.ID = raw.ID
	r.Model = raw.Model
	r.Provider = raw.Provider
	r.Results = raw.Results
	r.Usage = raw.Usage
	r.ExtraFields = extraFields
	return nil
}

func (r RerankResponse) MarshalJSON() ([]byte, error) {
	type rerankResponseAlias struct {
		ID       string         `json:"id"`
		Model    string         `json:"model"`
		Provider string         `json:"provider"`
		Results  []RerankResult `json:"results"`
		Usage    RerankUsage    `json:"usage"`
	}

	return marshalWithUnknownJSONFields(rerankResponseAlias{
		ID:       r.ID,
		Model:    r.Model,
		Provider: r.Provider,
		Results:  r.Results,
		Usage:    r.Usage,
	}, r.ExtraFields)
}
