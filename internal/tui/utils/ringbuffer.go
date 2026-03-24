package utils

import (
	"sync"
)

// RingBuffer is a fixed-size circular buffer that automatically overwrites old entries
// when capacity is exceeded. It provides O(1) amortized time complexity for Add operations
// and maintains insertion order. Thread-safe for concurrent access.
type RingBuffer[T any] struct {
	data     []T
	capacity int
	size     int
	head     int
	mu       sync.RWMutex
}

// NewRingBuffer creates a new ring buffer with the specified capacity.
// If capacity is 0 or negative, it defaults to 1 to ensure valid operation.
func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
	if capacity <= 0 {
		capacity = 1
	}
	return &RingBuffer[T]{
		data:     make([]T, capacity),
		capacity: capacity,
		size:     0,
		head:     0,
	}
}

// Add adds an item to the buffer. If the buffer is full, the oldest item
// is overwritten (FIFO behavior).
func (rb *RingBuffer[T]) Add(item T) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.data[rb.head] = item
	rb.head = (rb.head + 1) % rb.capacity

	if rb.size < rb.capacity {
		rb.size++
	}
}

// GetAll returns all items in the buffer in insertion order (oldest to newest).
// Returns a new slice to avoid exposing internal state.
func (rb *RingBuffer[T]) GetAll() []T {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.getAllLocked()
}

// getAllLocked returns all items without acquiring the lock. Caller must hold at least RLock.
func (rb *RingBuffer[T]) getAllLocked() []T {
	if rb.size == 0 {
		return make([]T, 0)
	}

	result := make([]T, rb.size)
	for i := 0; i < rb.size; i++ {
		idx := (rb.head - rb.size + i + rb.capacity) % rb.capacity
		result[i] = rb.data[idx]
	}
	return result
}

// Get returns the last N items from the buffer. If count is greater than
// the current size, returns all available items. If count is 0 or negative,
// returns all items. Items are ordered from oldest to newest.
func (rb *RingBuffer[T]) Get(count int) []T {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if count <= 0 || count >= rb.size {
		return rb.getAllLocked()
	}

	result := make([]T, count)
	start := rb.size - count
	for i := 0; i < count; i++ {
		idx := (rb.head - rb.size + start + i + rb.capacity) % rb.capacity
		result[i] = rb.data[idx]
	}
	return result
}

// Clear removes all items from the buffer, resetting it to empty state
// while preserving the capacity.
func (rb *RingBuffer[T]) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.size = 0
	rb.head = 0
	var zero T
	for i := 0; i < rb.capacity; i++ {
		rb.data[i] = zero
	}
}

// Size returns the current number of items in the buffer.
func (rb *RingBuffer[T]) Size() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.size
}

// Capacity returns the maximum number of items the buffer can hold.
func (rb *RingBuffer[T]) Capacity() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.capacity
}

// IsFull returns true if the buffer has reached its maximum capacity.
func (rb *RingBuffer[T]) IsFull() bool {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.size == rb.capacity
}

// IsEmpty returns true if the buffer contains no items.
func (rb *RingBuffer[T]) IsEmpty() bool {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.size == 0
}

// ToSlice is an alias for GetAll() that returns items in insertion order.
func (rb *RingBuffer[T]) ToSlice() []T {
	return rb.GetAll()
}

// Peek returns the most recently added item without removing it.
// Returns the zero value of T if the buffer is empty.
func (rb *RingBuffer[T]) Peek() T {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.size == 0 {
		var zero T
		return zero
	}

	idx := (rb.head - 1 + rb.capacity) % rb.capacity
	return rb.data[idx]
}

// PeekFirst returns the oldest item in the buffer without removing it.
// Returns the zero value of T if the buffer is empty.
func (rb *RingBuffer[T]) PeekFirst() T {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.size == 0 {
		var zero T
		return zero
	}

	idx := (rb.head - rb.size + rb.capacity) % rb.capacity
	return rb.data[idx]
}
