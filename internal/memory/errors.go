package memory

import (
	"errors"
)

// 定义错误
var (
	ErrNilItem      = errors.New("memory item is nil")
	ErrNotFound     = errors.New("memory item not found")
	ErrExpired      = errors.New("memory item expired")
	ErrInvalidType  = errors.New("invalid memory type")
	ErrInvalidQuery = errors.New("invalid query")
	ErrFull         = errors.New("memory is full")
)
