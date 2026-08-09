package reflection

import (
	"context"
	"encoding/json"
	"fmt"

	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/utils/file"
)

// FileExperienceStore 基于文件的经验存储
type FileExperienceStore struct {
	config    *ReflectionConfig
	cache     map[string]*Experience
	fileIndex map[string]string // ID -> 文件路径
	logger    *Logger
	mu        sync.RWMutex

	// 统计信息
	totalSaved   int64
	totalLoaded  int64
	totalDeleted int64
}

// NewFileExperienceStore 创建文件经验存储
func NewFileExperienceStore(config *ReflectionConfig) (*FileExperienceStore, error) {
	if config == nil {
		config = DefaultReflectionConfig()
	}

	store := &FileExperienceStore{
		config:    config,
		cache:     make(map[string]*Experience),
		fileIndex: make(map[string]string),
		logger:    GetLogger(),
	}

	// 确保存储目录存在
	if err := os.MkdirAll(config.StoragePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	// 加载现有经验到索引
	if err := store.loadIndex(); err != nil {
		return nil, fmt.Errorf("failed to load index: %w", err)
	}

	store.logger.Info("Experience store initialized at: %s", config.StoragePath)
	store.logger.Info("Indexed %d existing experiences", len(store.fileIndex))

	return store, nil
}

// Save 保存经验
func (s *FileExperienceStore) Save(ctx context.Context, experience *Experience) error {
	start := time.Now()

	if experience == nil {
		return fmt.Errorf("experience is nil")
	}

	if experience.ID == "" {
		return fmt.Errorf("experience ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 序列化为JSON
	data, err := json.MarshalIndent(experience, "", "  ")
	if err != nil {
		s.logger.Error("Failed to marshal experience %s: %v", experience.ID, err)
		return fmt.Errorf("failed to marshal experience: %w", err)
	}

	// 生成文件名
	filename := fmt.Sprintf("%s.json", experience.ID)
	filePath := filepath.Join(s.config.StoragePath, filename)

	// 写入文件
	if err := file.AtomicWrite(filePath, data, 0644); err != nil {
		s.logger.Error("Failed to write experience file %s: %v", filePath, err)
		return fmt.Errorf("failed to write experience file: %w", err)
	}

	// 更新索引和缓存
	s.fileIndex[experience.ID] = filePath
	s.cache[experience.ID] = experience

	// 更新统计
	s.totalSaved++

	// 记录日志
	duration := time.Since(start)
	s.logger.LogExperienceSave(experience.ID, experience.Type, experience.Effectiveness)
	s.logger.Debug(" Save duration: %v", duration)

	return nil
}

// Load 加载经验
func (s *FileExperienceStore) Load(ctx context.Context, experienceID string) (*Experience, error) {
	if experienceID == "" {
		return nil, fmt.Errorf("experience ID is required")
	}

	s.mu.RLock()
	// 先检查缓存
	if exp, exists := s.cache[experienceID]; exists {
		s.mu.RUnlock()
		s.totalLoaded++
		return exp, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	// 获取文件路径
	filePath, exists := s.fileIndex[experienceID]
	if !exists {
		return nil, fmt.Errorf("experience %s not found", experienceID)
	}

	// 读取文件
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read experience file: %w", err)
	}

	// 反序列化
	var experience Experience
	if err := json.Unmarshal(data, &experience); err != nil {
		return nil, fmt.Errorf("failed to unmarshal experience: %w", err)
	}

	// 更新缓存
	s.cache[experienceID] = &experience

	// 更新统计
	s.totalLoaded++

	return &experience, nil
}

// Search 搜索相似经验
func (s *FileExperienceStore) Search(ctx context.Context, query *ExperienceQuery) ([]*Experience, error) {
	start := time.Now()

	if query == nil {
		return nil, fmt.Errorf("query is nil")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]*Experience, 0)

	// 遍历所有经验
	for _, filePath := range s.fileIndex {
		// 读取经验
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue // 跳过无法读取的文件
		}

		var experience Experience
		if err := json.Unmarshal(data, &experience); err != nil {
			continue // 跳过无法解析的文件
		}

		// 应用过滤条件
		if s.matchesQuery(&experience, query) {
			results = append(results, &experience)

			// 检查limit
			if query.Limit > 0 && len(results) >= query.Limit {
				break
			}
		}
	}

	// 按有效性排序
	s.sortByEffectiveness(results)

	// 应用offset
	if query.Offset > 0 && query.Offset < len(results) {
		results = results[query.Offset:]
	}

	// 记录搜索日志
	duration := time.Since(start)
	s.logger.LogExperienceSearch(query, len(results), duration)

	return results, nil
}

// Update 更新经验
func (s *FileExperienceStore) Update(ctx context.Context, experience *Experience) error {
	// 更新就是重新保存
	return s.Save(ctx, experience)
}

// Delete 删除经验
func (s *FileExperienceStore) Delete(ctx context.Context, experienceID string) error {
	if experienceID == "" {
		return fmt.Errorf("experience ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 获取文件路径
	filePath, exists := s.fileIndex[experienceID]
	if !exists {
		return fmt.Errorf("experience %s not found", experienceID)
	}

	// 删除文件
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to delete experience file: %w", err)
	}

	// 更新索引和缓存
	delete(s.fileIndex, experienceID)
	delete(s.cache, experienceID)

	// 更新统计
	s.totalDeleted++

	return nil
}

// GetMostRelevant 获取最相关的经验
func (s *FileExperienceStore) GetMostRelevant(ctx context.Context, context *ReflectionContext, limit int) ([]*Experience, error) {
	if context == nil {
		return nil, fmt.Errorf("context is nil")
	}

	// 构建查询
	query := &ExperienceQuery{
		Keywords:         s.extractKeywords(context),
		MinEffectiveness: s.config.MinEffectiveness,
		MaxAge:           s.config.MaxExperienceAge,
		Limit:            limit,
	}

	return s.Search(ctx, query)
}

// matchesQuery 检查经验是否匹配查询条件
func (s *FileExperienceStore) matchesQuery(experience *Experience, query *ExperienceQuery) bool {
	// 类型过滤
	if query.Type != "" && experience.Type != query.Type {
		return false
	}

	// 上下文匹配
	if query.Context != "" && !strings.Contains(experience.Context, query.Context) {
		return false
	}

	// 关键词匹配
	if len(query.Keywords) > 0 {
		matched := false
		for _, keyword := range query.Keywords {
			if s.containsKeyword(experience, keyword) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// 标签匹配
	if len(query.Tags) > 0 {
		matched := false
		for _, tag := range query.Tags {
			for _, expTag := range experience.Tags {
				if strings.Contains(expTag, tag) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			return false
		}
	}

	// 有效性过滤
	if query.MinEffectiveness > 0 && experience.Effectiveness < query.MinEffectiveness {
		return false
	}

	// 年龄过滤
	if query.MaxAge > 0 {
		age := time.Since(experience.Timestamp)
		if age > query.MaxAge {
			return false
		}
	}

	return true
}

// containsKeyword 检查经验是否包含关键词
func (s *FileExperienceStore) containsKeyword(experience *Experience, keyword string) bool {
	keyword = strings.ToLower(keyword)

	// 检查各种字段
	if strings.Contains(strings.ToLower(experience.Context), keyword) {
		return true
	}

	if strings.Contains(strings.ToLower(experience.Goal), keyword) {
		return true
	}

	if strings.Contains(strings.ToLower(experience.Action), keyword) {
		return true
	}

	if strings.Contains(strings.ToLower(experience.Result), keyword) {
		return true
	}

	for _, kw := range experience.Keywords {
		if strings.Contains(strings.ToLower(kw), keyword) {
			return true
		}
	}

	return false
}

// extractKeywords 从上下文提取关键词
func (s *FileExperienceStore) extractKeywords(context *ReflectionContext) []string {
	keywords := make([]string, 0)

	// 从目标中提取
	if context.Goal != "" {
		words := strings.Fields(context.Goal)
		for _, word := range words {
			if len(word) > 3 { // 只保留长度大于3的词
				keywords = append(keywords, strings.ToLower(word))
			}
		}
	}

	// 从任务类型中提取
	if context.TaskType != "" {
		keywords = append(keywords, strings.ToLower(context.TaskType))
	}

	return keywords
}

// sortByEffectiveness 按有效性排序
func (s *FileExperienceStore) sortByEffectiveness(experiences []*Experience) {
	// 简单的冒泡排序
	for i := 0; i < len(experiences)-1; i++ {
		for j := 0; j < len(experiences)-i-1; j++ {
			if experiences[j].Effectiveness < experiences[j+1].Effectiveness {
				experiences[j], experiences[j+1] = experiences[j+1], experiences[j]
			}
		}
	}
}

// loadIndex 加载索引
func (s *FileExperienceStore) loadIndex() error {
	// 检查目录是否存在
	if _, err := os.Stat(s.config.StoragePath); os.IsNotExist(err) {
		return nil // 目录不存在，返回空索引
	}

	// 遍历目录中的所有JSON文件
	files, err := os.ReadDir(s.config.StoragePath)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		// 从文件名提取ID
		expID := strings.TrimSuffix(file.Name(), ".json")
		filePath := filepath.Join(s.config.StoragePath, file.Name())

		s.fileIndex[expID] = filePath
	}

	return nil
}

// GetStats 获取统计信息
func (s *FileExperienceStore) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"total_saved":   s.totalSaved,
		"total_loaded":  s.totalLoaded,
		"total_deleted": s.totalDeleted,
		"cached_items":  len(s.cache),
		"indexed_items": len(s.fileIndex),
		"storage_path":  s.config.StoragePath,
	}
}

// Clear 清空所有经验
func (s *FileExperienceStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 删除所有文件
	for id, filePath := range s.fileIndex {
		if err := os.Remove(filePath); err != nil {
			// 记录错误但继续
			fmt.Printf("Failed to delete %s: %v\n", filePath, err)
		}
		delete(s.fileIndex, id)
	}

	// 清空缓存
	s.cache = make(map[string]*Experience)

	return nil
}

// Export 导出所有经验
func (s *FileExperienceStore) Export(ctx context.Context, outputPath string) error {
	s.mu.RLock()
	ids := make([]string, 0, len(s.fileIndex))
	for id := range s.fileIndex {
		ids = append(ids, id)
	}
	s.mu.RUnlock()

	experiences := make([]*Experience, 0, len(ids))
	for _, id := range ids {
		exp, err := s.Load(ctx, id)
		if err != nil {
			continue
		}
		experiences = append(experiences, exp)
	}

	data, err := json.MarshalIndent(experiences, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal experiences: %w", err)
	}

	if err := file.AtomicWrite(outputPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write export file: %w", err)
	}

	return nil
}

// Import 导入经验
func (s *FileExperienceStore) Import(ctx context.Context, inputPath string) error {
	// 读取文件
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("failed to read import file: %w", err)
	}

	// 反序列化
	var experiences []*Experience
	if err := json.Unmarshal(data, &experiences); err != nil {
		return fmt.Errorf("failed to unmarshal experiences: %w", err)
	}

	// 保存所有经验
	for _, exp := range experiences {
		if err := s.Save(ctx, exp); err != nil {
			// 记录错误但继续
			fmt.Printf("Failed to save experience %s: %v\n", exp.ID, err)
		}
	}

	return nil
}
