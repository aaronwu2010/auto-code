package skills

import (
	"context"
	"sync"
)

type SkillLoader struct {
	mu            sync.RWMutex
	bundledSkills map[string]*BundledSkillDefinition
	skillDir      string
}

func NewSkillLoader(skillDir string) *SkillLoader {
	return &SkillLoader{
		bundledSkills: make(map[string]*BundledSkillDefinition),
		skillDir:      skillDir,
	}
}

func (l *SkillLoader) LoadBundledSkills(_ context.Context) error {
	bundled := GetBundledSkills()
	for _, skill := range bundled {
		l.bundledSkills[skill.Name] = skill
	}
	return nil
}

func (l *SkillLoader) LoadSkillDirSkills(_ context.Context) ([]*BundledSkillDefinition, error) {
	return make([]*BundledSkillDefinition, 0), nil
}

func (l *SkillLoader) GetSkills() []*BundledSkillDefinition {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]*BundledSkillDefinition, 0, len(l.bundledSkills))
	for _, s := range l.bundledSkills {
		result = append(result, s)
	}
	return result
}

func (l *SkillLoader) FindSkill(name string) *BundledSkillDefinition {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.bundledSkills[name]
}

func (l *SkillLoader) GetSkillToolCommands() []*BundledSkillDefinition {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]*BundledSkillDefinition, 0)
	for _, s := range l.bundledSkills {
		if s.Type == "prompt" && !s.DisableModel {
			result = append(result, s)
		}
	}
	return result
}
