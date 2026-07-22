package policylimits

type PolicyRestriction struct {
	Allowed bool `json:"allowed"`
}

type PolicyLimitsResponse struct {
	Restrictions map[string]PolicyRestriction `json:"restrictions"`
}

type PolicyLimitsFetchResult struct {
	Success      bool                                `json:"success"`
	Restrictions map[string]PolicyRestriction        `json:"restrictions,omitempty"`
	ETag         string                              `json:"etag,omitempty"`
	Error        string                              `json:"error,omitempty"`
	SkipRetry    bool                                `json:"skipRetry,omitempty"`
}