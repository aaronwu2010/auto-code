package analytics

import (
	"sync"
	"time"
)

type GrowthBookFeatureType string

const (
	FeatureTypeBool       GrowthBookFeatureType = "bool"
	FeatureTypeString     GrowthBookFeatureType = "string"
	FeatureTypeNumber     GrowthBookFeatureType = "number"
	FeatureTypeJSON       GrowthBookFeatureType = "json"
)

type GrowthBookFeature struct {
	ID             string                 `json:"id"`
	DefaultValue   interface{}            `json:"defaultValue"`
	CurrentValue   interface{}            `json:"currentValue"`
	FeatureType    GrowthBookFeatureType  `json:"featureType"`
	IsOverridden   bool                   `json:"isOverridden"`
}

type GrowthBookUserAttributes struct {
	ID             string `json:"id"`
	Environment    string `json:"environment,omitempty"`
	SubscriptionType string `json:"subscriptionType,omitempty"`
	OrganizationID string `json:"organizationId,omitempty"`
}

type GrowthBookService struct {
	mu          sync.RWMutex
	features    map[string]*GrowthBookFeature
	overrides   map[string]interface{}
	userAttrs   GrowthBookUserAttributes
	refreshMu   sync.Mutex
	lastRefresh time.Time
	cancelFunc  func()
}

func NewGrowthBookService(userAttrs GrowthBookUserAttributes) *GrowthBookService {
	return &GrowthBookService{
		features:  make(map[string]*GrowthBookFeature),
		overrides: make(map[string]interface{}),
		userAttrs: userAttrs,
	}
}

func (g *GrowthBookService) GetFeatureValue(featureKey string, defaultValue interface{}) interface{} {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if override, ok := g.overrides[featureKey]; ok {
		return override
	}

	if feature, ok := g.features[featureKey]; ok {
		if feature.CurrentValue != nil {
			return feature.CurrentValue
		}
	}
	return defaultValue
}

func (g *GrowthBookService) GetFeatureValueBool(featureKey string, defaultValue bool) bool {
	val := g.GetFeatureValue(featureKey, defaultValue)
	if b, ok := val.(bool); ok {
		return b
	}
	return defaultValue
}

func (g *GrowthBookService) GetFeatureValueString(featureKey string, defaultValue string) string {
	val := g.GetFeatureValue(featureKey, defaultValue)
	if s, ok := val.(string); ok {
		return s
	}
	return defaultValue
}

func (g *GrowthBookService) GetFeatureValueInt(featureKey string, defaultValue int) int {
	val := g.GetFeatureValue(featureKey, defaultValue)
	if n, ok := val.(float64); ok {
		return int(n)
	}
	if n, ok := val.(int); ok {
		return n
	}
	return defaultValue
}

func (g *GrowthBookService) SetConfigOverride(featureKey string, value interface{}) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.overrides[featureKey] = value
}

func (g *GrowthBookService) ClearConfigOverride(featureKey string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.overrides, featureKey)
}

func (g *GrowthBookService) ClearAllOverrides() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.overrides = make(map[string]interface{})
}

func (g *GrowthBookService) GetAllFeatures() map[string]*GrowthBookFeature {
	g.mu.RLock()
	defer g.mu.RUnlock()
	result := make(map[string]*GrowthBookFeature, len(g.features))
	for k, v := range g.features {
		result[k] = v
	}
	return result
}

func (g *GrowthBookService) HasEnvOverride() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.overrides) > 0
}

func (g *GrowthBookService) UpdateFeatures(features map[string]*GrowthBookFeature) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.features = features
	g.lastRefresh = time.Now()
}

func (g *GrowthBookService) RefreshAfterAuthChange(subscriptionType, orgID string) {
	g.mu.Lock()
	g.userAttrs.SubscriptionType = subscriptionType
	g.userAttrs.OrganizationID = orgID
	g.mu.Unlock()
}

func (g *GrowthBookService) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.features = make(map[string]*GrowthBookFeature)
	g.overrides = make(map[string]interface{})
	g.lastRefresh = time.Time{}
}

func (g *GrowthBookService) CheckGate(featureKey string) bool {
	return g.GetFeatureValueBool(featureKey, false)
}

func (g *GrowthBookService) CheckSecurityRestrictionGate(featureKey string) bool {
	return g.GetFeatureValueBool(featureKey, false)
}