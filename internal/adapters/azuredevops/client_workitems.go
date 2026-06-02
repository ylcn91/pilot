package azuredevops

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Work Item API methods

// GetWorkItem fetches a work item by ID
func (c *Client) GetWorkItem(ctx context.Context, id int) (*WorkItem, error) {
	path := fmt.Sprintf("/%s/%s/_apis/wit/workitems/%d?api-version=%s",
		url.PathEscape(c.organization),
		url.PathEscape(c.project),
		id,
		apiVersion,
	)
	var wi WorkItem
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &wi); err != nil {
		return nil, err
	}
	return &wi, nil
}

// ListWorkItemsByWIQL executes a WIQL query and returns work items
func (c *Client) ListWorkItemsByWIQL(ctx context.Context, wiql string) ([]*WorkItem, error) {
	path := fmt.Sprintf("/%s/%s/_apis/wit/wiql?api-version=%s",
		url.PathEscape(c.organization),
		url.PathEscape(c.project),
		apiVersion,
	)

	reqBody := map[string]string{"query": wiql}
	var queryResult WIQLQueryResult
	if err := c.doRequest(ctx, http.MethodPost, path, reqBody, &queryResult); err != nil {
		return nil, fmt.Errorf("failed to execute WIQL query: %w", err)
	}

	if len(queryResult.WorkItems) == 0 {
		return []*WorkItem{}, nil
	}

	// Get work item IDs
	ids := make([]int, len(queryResult.WorkItems))
	for i, ref := range queryResult.WorkItems {
		ids[i] = ref.ID
	}

	// Fetch full work item details
	return c.GetWorkItems(ctx, ids)
}

// GetWorkItems fetches multiple work items by IDs
func (c *Client) GetWorkItems(ctx context.Context, ids []int) ([]*WorkItem, error) {
	if len(ids) == 0 {
		return []*WorkItem{}, nil
	}

	// Azure DevOps limits to 200 items per request
	const batchSize = 200
	var allWorkItems []*WorkItem

	for i := 0; i < len(ids); i += batchSize {
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}

		batch := ids[i:end]
		idStrs := make([]string, len(batch))
		for j, id := range batch {
			idStrs[j] = strconv.Itoa(id)
		}

		path := fmt.Sprintf("/%s/%s/_apis/wit/workitems?ids=%s&api-version=%s",
			url.PathEscape(c.organization),
			url.PathEscape(c.project),
			strings.Join(idStrs, ","),
			apiVersion,
		)

		var result struct {
			Count int         `json:"count"`
			Value []*WorkItem `json:"value"`
		}
		if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
			return nil, err
		}

		allWorkItems = append(allWorkItems, result.Value...)
	}

	return allWorkItems, nil
}

// ListWorkItems lists work items with the specified options
func (c *Client) ListWorkItems(ctx context.Context, opts *ListWorkItemsOptions) ([]*WorkItem, error) {
	// Build WIQL query
	wiql := "SELECT [System.Id] FROM WorkItems WHERE "
	conditions := []string{}

	// Filter by tags
	if opts != nil && len(opts.Tags) > 0 {
		for _, tag := range opts.Tags {
			conditions = append(conditions, fmt.Sprintf("[System.Tags] CONTAINS '%s'", tag))
		}
	}

	// Filter by states (exclude specified states)
	if opts != nil && len(opts.States) > 0 {
		stateConditions := []string{}
		for _, state := range opts.States {
			stateConditions = append(stateConditions, fmt.Sprintf("[System.State] = '%s'", state))
		}
		conditions = append(conditions, "("+strings.Join(stateConditions, " OR ")+")")
	}

	// Filter by work item types
	if opts != nil && len(opts.WorkItemTypes) > 0 {
		typeConditions := []string{}
		for _, wit := range opts.WorkItemTypes {
			typeConditions = append(typeConditions, fmt.Sprintf("[System.WorkItemType] = '%s'", wit))
		}
		conditions = append(conditions, "("+strings.Join(typeConditions, " OR ")+")")
	}

	// Filter by updated date
	if opts != nil && !opts.UpdatedAfter.IsZero() {
		conditions = append(conditions, fmt.Sprintf("[System.ChangedDate] >= '%s'", opts.UpdatedAfter.Format("2006-01-02T15:04:05Z")))
	}

	if len(conditions) == 0 {
		wiql += "1=1"
	} else {
		wiql += strings.Join(conditions, " AND ")
	}

	wiql += " ORDER BY [System.CreatedDate] ASC"

	return c.ListWorkItemsByWIQL(ctx, wiql)
}

// PatchOperation represents a JSON Patch operation
type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
	From  string      `json:"from,omitempty"`
}

// UpdateWorkItem updates a work item with JSON Patch operations
func (c *Client) UpdateWorkItem(ctx context.Context, id int, operations []PatchOperation) (*WorkItem, error) {
	path := fmt.Sprintf("/%s/%s/_apis/wit/workitems/%d?api-version=%s",
		url.PathEscape(c.organization),
		url.PathEscape(c.project),
		id,
		apiVersion,
	)

	var wi WorkItem
	if err := c.doRequestWithPatch(ctx, path, operations, &wi); err != nil {
		return nil, err
	}
	return &wi, nil
}

// AddWorkItemTag adds a tag to a work item
func (c *Client) AddWorkItemTag(ctx context.Context, id int, tag string) error {
	// First get current tags
	wi, err := c.GetWorkItem(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get work item: %w", err)
	}

	currentTags := ""
	if tags, ok := wi.Fields["System.Tags"].(string); ok {
		currentTags = tags
	}

	newTags := addTag(currentTags, tag)
	if newTags == currentTags {
		return nil // Tag already exists
	}

	ops := []PatchOperation{
		{
			Op:    "add",
			Path:  "/fields/System.Tags",
			Value: newTags,
		},
	}

	_, err = c.UpdateWorkItem(ctx, id, ops)
	return err
}

// RemoveWorkItemTag removes a tag from a work item
func (c *Client) RemoveWorkItemTag(ctx context.Context, id int, tag string) error {
	// First get current tags
	wi, err := c.GetWorkItem(ctx, id)
	if err != nil {
		// If work item doesn't exist, nothing to do
		return nil
	}

	currentTags := ""
	if tags, ok := wi.Fields["System.Tags"].(string); ok {
		currentTags = tags
	}

	newTags := removeTag(currentTags, tag)
	if newTags == currentTags {
		return nil // Tag wasn't there
	}

	ops := []PatchOperation{
		{
			Op:    "add",
			Path:  "/fields/System.Tags",
			Value: newTags,
		},
	}

	_, err = c.UpdateWorkItem(ctx, id, ops)
	return err
}

// AddWorkItemComment adds a comment to a work item
func (c *Client) AddWorkItemComment(ctx context.Context, id int, text string) (*Comment, error) {
	path := fmt.Sprintf("/%s/%s/_apis/wit/workitems/%d/comments?api-version=%s-preview",
		url.PathEscape(c.organization),
		url.PathEscape(c.project),
		id,
		apiVersion,
	)

	reqBody := map[string]string{"text": text}
	var comment Comment
	if err := c.doRequest(ctx, http.MethodPost, path, reqBody, &comment); err != nil {
		return nil, err
	}
	return &comment, nil
}

// UpdateWorkItemState updates a work item's state
func (c *Client) UpdateWorkItemState(ctx context.Context, id int, state string) error {
	ops := []PatchOperation{
		{
			Op:    "add",
			Path:  "/fields/System.State",
			Value: state,
		},
	}

	_, err := c.UpdateWorkItem(ctx, id, ops)
	return err
}
