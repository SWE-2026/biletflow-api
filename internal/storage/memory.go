package storage

import (
	"bytes"
	"context"
	"sync"
)

type Memory struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func NewMemory() *Memory {
	return &Memory{objects: map[string][]byte{}}
}

func (m *Memory) Put(_ context.Context, key, _ string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = bytes.Clone(data)
	return nil
}

func (m *Memory) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, ErrNotFound
	}
	return bytes.Clone(data), nil
}

func (m *Memory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *Memory) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.objects)
}
