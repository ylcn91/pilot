package azuredevops

import "time"

// WIQLQueryResult represents the result of a WIQL query
type WIQLQueryResult struct {
	QueryType         string                 `json:"queryType"`
	QueryResultType   string                 `json:"queryResultType"`
	AsOf              time.Time              `json:"asOf"`
	Columns           []WIQLColumn           `json:"columns"`
	WorkItems         []WIQLWorkItemRef      `json:"workItems"`
	WorkItemRelations []WIQLWorkItemRelation `json:"workItemRelations,omitempty"`
}

// WIQLColumn represents a column in WIQL results
type WIQLColumn struct {
	ReferenceName string `json:"referenceName"`
	Name          string `json:"name"`
	URL           string `json:"url"`
}

// WIQLWorkItemRef represents a work item reference in WIQL results
type WIQLWorkItemRef struct {
	ID  int    `json:"id"`
	URL string `json:"url"`
}

// WIQLWorkItemRelation represents a work item relation in WIQL results
type WIQLWorkItemRelation struct {
	Target *WIQLWorkItemRef `json:"target"`
	Rel    string           `json:"rel,omitempty"`
	Source *WIQLWorkItemRef `json:"source,omitempty"`
}
