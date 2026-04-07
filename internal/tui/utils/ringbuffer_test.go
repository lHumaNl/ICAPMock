// Copyright 2026 ICAP Mock

package utils

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRingBuffer(t *testing.T) {
	t.Run("creates buffer with valid capacity", func(t *testing.T) {
		rb := NewRingBuffer[int](10)
		assert.Equal(t, 10, rb.Capacity())
		assert.Equal(t, 0, rb.Size())
		assert.True(t, rb.IsEmpty())
		assert.False(t, rb.IsFull())
	})

	t.Run("defaults to capacity 1 for non-positive capacity", func(t *testing.T) {
		rb1 := NewRingBuffer[int](0)
		assert.Equal(t, 1, rb1.Capacity())

		rb2 := NewRingBuffer[int](-5)
		assert.Equal(t, 1, rb2.Capacity())
	})

	t.Run("works with different types", func(t *testing.T) {
		rbInt := NewRingBuffer[int](5)
		assert.NotNil(t, rbInt)

		rbString := NewRingBuffer[string](5)
		assert.NotNil(t, rbString)

		type TestStruct struct {
			Name string
			Val  int
		}
		rbStruct := NewRingBuffer[TestStruct](5)
		assert.NotNil(t, rbStruct)
	})
}

func TestRingBuffer_Add(t *testing.T) {
	t.Run("adds items to empty buffer", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		rb.Add(1)
		assert.Equal(t, 1, rb.Size())
		assert.False(t, rb.IsEmpty())
	})

	t.Run("adds multiple items", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		rb.Add(1)
		rb.Add(2)
		rb.Add(3)
		assert.Equal(t, 3, rb.Size())
	})

	t.Run("wraps around when capacity exceeded", func(t *testing.T) {
		rb := NewRingBuffer[int](3)
		rb.Add(1)
		rb.Add(2)
		rb.Add(3)
		rb.Add(4)
		rb.Add(5)

		assert.Equal(t, 3, rb.Size())
		assert.True(t, rb.IsFull())
	})

	t.Run("overwrites oldest items in FIFO order", func(t *testing.T) {
		rb := NewRingBuffer[int](3)
		rb.Add(1)
		rb.Add(2)
		rb.Add(3)
		rb.Add(4)

		items := rb.GetAll()
		assert.Equal(t, []int{2, 3, 4}, items)
	})
}

func TestRingBuffer_GetAll(t *testing.T) {
	t.Run("returns empty slice for empty buffer", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		items := rb.GetAll()
		assert.Empty(t, items)
	})

	t.Run("returns all items in insertion order", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		rb.Add(1)
		rb.Add(2)
		rb.Add(3)

		items := rb.GetAll()
		assert.Equal(t, []int{1, 2, 3}, items)
	})

	t.Run("maintains order after wrap-around", func(t *testing.T) {
		rb := NewRingBuffer[int](3)
		rb.Add(1)
		rb.Add(2)
		rb.Add(3)
		rb.Add(4)
		rb.Add(5)

		items := rb.GetAll()
		assert.Equal(t, []int{3, 4, 5}, items)
	})

	t.Run("returns new slice, not reference to internal data", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		rb.Add(1)
		rb.Add(2)

		items1 := rb.GetAll()
		items1[0] = 999

		items2 := rb.GetAll()
		assert.Equal(t, 1, items2[0])
	})
}

func TestRingBuffer_Get(t *testing.T) {
	t.Run("returns all items when count >= size", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		rb.Add(1)
		rb.Add(2)
		rb.Add(3)

		items := rb.Get(10)
		assert.Equal(t, []int{1, 2, 3}, items)
	})

	t.Run("returns all items when count <= 0", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		rb.Add(1)
		rb.Add(2)
		rb.Add(3)

		items := rb.Get(0)
		assert.Equal(t, []int{1, 2, 3}, items)

		items = rb.Get(-5)
		assert.Equal(t, []int{1, 2, 3}, items)
	})

	t.Run("returns last N items", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		rb.Add(1)
		rb.Add(2)
		rb.Add(3)
		rb.Add(4)
		rb.Add(5)

		items := rb.Get(2)
		assert.Equal(t, []int{4, 5}, items)
	})

	t.Run("returns last N items after wrap-around", func(t *testing.T) {
		rb := NewRingBuffer[int](3)
		rb.Add(1)
		rb.Add(2)
		rb.Add(3)
		rb.Add(4)
		rb.Add(5)

		items := rb.Get(2)
		assert.Equal(t, []int{4, 5}, items)
	})

	t.Run("returns empty slice for empty buffer", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		items := rb.Get(10)
		assert.Empty(t, items)
	})
}

func TestRingBuffer_Clear(t *testing.T) {
	t.Run("empties buffer", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		rb.Add(1)
		rb.Add(2)
		rb.Add(3)

		rb.Clear()

		assert.Equal(t, 0, rb.Size())
		assert.True(t, rb.IsEmpty())
		assert.False(t, rb.IsFull())
	})

	t.Run("resets head position", func(t *testing.T) {
		rb := NewRingBuffer[int](3)
		rb.Add(1)
		rb.Add(2)
		rb.Add(3)
		rb.Add(4)
		rb.Add(5)

		rb.Clear()

		rb.Add(10)
		rb.Add(20)

		items := rb.GetAll()
		assert.Equal(t, []int{10, 20}, items)
	})

	t.Run("preserves capacity after clear", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		rb.Add(1)
		rb.Add(2)
		rb.Add(3)

		rb.Clear()

		assert.Equal(t, 5, rb.Capacity())
	})
}

func TestRingBuffer_SizeAndCapacity(t *testing.T) {
	t.Run("reports correct size", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		assert.Equal(t, 0, rb.Size())

		rb.Add(1)
		assert.Equal(t, 1, rb.Size())

		rb.Add(2)
		rb.Add(3)
		rb.Add(4)
		assert.Equal(t, 4, rb.Size())

		rb.Add(5)
		rb.Add(6)
		assert.Equal(t, 5, rb.Size())
	})

	t.Run("reports correct capacity", func(t *testing.T) {
		rb := NewRingBuffer[int](100)
		assert.Equal(t, 100, rb.Capacity())

		rb.Add(1)
		rb.Add(2)
		assert.Equal(t, 100, rb.Capacity())
	})
}

func TestRingBuffer_IsFull(t *testing.T) {
	t.Run("returns false when empty", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		assert.False(t, rb.IsFull())
	})

	t.Run("returns false when partially filled", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		rb.Add(1)
		rb.Add(2)
		assert.False(t, rb.IsFull())
	})

	t.Run("returns true when at capacity", func(t *testing.T) {
		rb := NewRingBuffer[int](3)
		rb.Add(1)
		rb.Add(2)
		rb.Add(3)
		assert.True(t, rb.IsFull())
	})

	t.Run("remains true after wrap-around", func(t *testing.T) {
		rb := NewRingBuffer[int](3)
		rb.Add(1)
		rb.Add(2)
		rb.Add(3)
		rb.Add(4)
		assert.True(t, rb.IsFull())
	})
}

func TestRingBuffer_IsEmpty(t *testing.T) {
	t.Run("returns true when empty", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		assert.True(t, rb.IsEmpty())
	})

	t.Run("returns false when has items", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		rb.Add(1)
		assert.False(t, rb.IsEmpty())
	})

	t.Run("returns false after clear and add", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		rb.Add(1)
		rb.Clear()
		assert.True(t, rb.IsEmpty())

		rb.Add(2)
		assert.False(t, rb.IsEmpty())
	})
}

func TestRingBuffer_ToSlice(t *testing.T) {
	t.Run("returns same as GetAll", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		rb.Add(1)
		rb.Add(2)
		rb.Add(3)

		items1 := rb.GetAll()
		items2 := rb.ToSlice()
		assert.Equal(t, items1, items2)
	})
}

func TestRingBuffer_Peek(t *testing.T) {
	t.Run("returns most recent item", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		rb.Add(1)
		rb.Add(2)
		rb.Add(3)

		item := rb.Peek()
		assert.Equal(t, 3, item)
	})

	t.Run("returns zero value for empty buffer", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		item := rb.Peek()
		assert.Equal(t, 0, item)
	})

	t.Run("works after wrap-around", func(t *testing.T) {
		rb := NewRingBuffer[int](3)
		rb.Add(1)
		rb.Add(2)
		rb.Add(3)
		rb.Add(4)
		rb.Add(5)

		item := rb.Peek()
		assert.Equal(t, 5, item)
	})

	t.Run("works with strings", func(t *testing.T) {
		rb := NewRingBuffer[string](5)
		rb.Add("hello")
		rb.Add("world")

		item := rb.Peek()
		assert.Equal(t, "world", item)
	})
}

func TestRingBuffer_PeekFirst(t *testing.T) {
	t.Run("returns oldest item", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		rb.Add(1)
		rb.Add(2)
		rb.Add(3)

		item := rb.PeekFirst()
		assert.Equal(t, 1, item)
	})

	t.Run("returns zero value for empty buffer", func(t *testing.T) {
		rb := NewRingBuffer[int](5)
		item := rb.PeekFirst()
		assert.Equal(t, 0, item)
	})

	t.Run("works after wrap-around", func(t *testing.T) {
		rb := NewRingBuffer[int](3)
		rb.Add(1)
		rb.Add(2)
		rb.Add(3)
		rb.Add(4)
		rb.Add(5)

		item := rb.PeekFirst()
		assert.Equal(t, 3, item)
	})
}

func TestRingBuffer_ConcurrentAccess(t *testing.T) {
	t.Run("concurrent Add operations are thread-safe", func(t *testing.T) {
		rb := NewRingBuffer[int](100)
		var wg sync.WaitGroup

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					rb.Add(id*100 + j)
				}
			}(i)
		}

		wg.Wait()

		assert.Equal(t, 100, rb.Size())
		assert.True(t, rb.IsFull())
	})

	t.Run("concurrent Add and Get operations are thread-safe", func(t *testing.T) {
		rb := NewRingBuffer[int](1000)
		var wg sync.WaitGroup

		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 200; j++ {
					rb.Add(j)
				}
			}()
		}

		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 200; j++ {
					rb.GetAll()
					rb.Get(10)
				}
			}()
		}

		wg.Wait()

		assert.Equal(t, 1000, rb.Size())
	})

	t.Run("concurrent Clear operations are thread-safe", func(t *testing.T) {
		rb := NewRingBuffer[int](100)
		var wg sync.WaitGroup

		for i := 0; i < 100; i++ {
			rb.Add(i)
		}

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rb.Clear()
				for j := 0; j < 20; j++ {
					rb.Add(j)
				}
			}()
		}

		wg.Wait()

		size := rb.Size()
		assert.True(t, size <= 100)
	})
}

func TestRingBuffer_EdgeCases(t *testing.T) {
	t.Run("capacity of 1", func(t *testing.T) {
		rb := NewRingBuffer[int](1)
		rb.Add(1)
		assert.Equal(t, 1, rb.Size())
		assert.Equal(t, []int{1}, rb.GetAll())

		rb.Add(2)
		assert.Equal(t, 1, rb.Size())
		assert.Equal(t, []int{2}, rb.GetAll())
	})

	t.Run("large capacity", func(t *testing.T) {
		rb := NewRingBuffer[int](10000)
		for i := 0; i < 10000; i++ {
			rb.Add(i)
		}

		assert.Equal(t, 10000, rb.Size())
		assert.True(t, rb.IsFull())

		items := rb.Get(10)
		assert.Equal(t, 10, len(items))
		assert.Equal(t, []int{9990, 9991, 9992, 9993, 9994, 9995, 9996, 9997, 9998, 9999}, items)
	})

	t.Run("alternating add and get", func(t *testing.T) {
		rb := NewRingBuffer[int](5)

		rb.Add(1)
		rb.Add(2)
		rb.Add(3)

		items := rb.Get(2)
		assert.Equal(t, []int{2, 3}, items)

		rb.Add(4)
		rb.Add(5)

		items = rb.GetAll()
		assert.Equal(t, []int{1, 2, 3, 4, 5}, items)

		rb.Add(6)

		items = rb.GetAll()
		assert.Equal(t, []int{2, 3, 4, 5, 6}, items)
	})
}

func TestRingBuffer_WithStructs(t *testing.T) {
	type Person struct {
		Name string
		Age  int
	}

	t.Run("stores and retrieves structs", func(t *testing.T) {
		rb := NewRingBuffer[Person](3)
		rb.Add(Person{"Alice", 30})
		rb.Add(Person{"Bob", 25})
		rb.Add(Person{"Charlie", 35})

		items := rb.GetAll()
		require.Len(t, items, 3)
		assert.Equal(t, "Alice", items[0].Name)
		assert.Equal(t, 30, items[0].Age)
		assert.Equal(t, "Bob", items[1].Name)
		assert.Equal(t, "Charlie", items[2].Name)
	})

	t.Run("wraps around with structs", func(t *testing.T) {
		rb := NewRingBuffer[Person](2)
		rb.Add(Person{"Alice", 30})
		rb.Add(Person{"Bob", 25})
		rb.Add(Person{"Charlie", 35})

		items := rb.GetAll()
		require.Len(t, items, 2)
		assert.Equal(t, "Bob", items[0].Name)
		assert.Equal(t, "Charlie", items[1].Name)
	})
}

func TestRingBuffer_WithPointers(t *testing.T) {
	t.Run("stores and retrieves pointers", func(t *testing.T) {
		rb := NewRingBuffer[*int](5)
		a, b, c := 1, 2, 3

		rb.Add(&a)
		rb.Add(&b)
		rb.Add(&c)

		items := rb.GetAll()
		require.Len(t, items, 3)
		assert.Equal(t, 1, *items[0])
		assert.Equal(t, 2, *items[1])
		assert.Equal(t, 3, *items[2])
	})
}

func TestRingBuffer_String(t *testing.T) {
	t.Run("stores and retrieves strings", func(t *testing.T) {
		rb := NewRingBuffer[string](3)
		rb.Add("hello")
		rb.Add("world")
		rb.Add("foo")

		items := rb.GetAll()
		assert.Equal(t, []string{"hello", "world", "foo"}, items)
	})

	t.Run("wraps around with strings", func(t *testing.T) {
		rb := NewRingBuffer[string](2)
		rb.Add("a")
		rb.Add("b")
		rb.Add("c")

		items := rb.GetAll()
		assert.Equal(t, []string{"b", "c"}, items)
	})
}

func BenchmarkRingBuffer_Add(b *testing.B) {
	rb := NewRingBuffer[int](1000)
	for i := 0; i < b.N; i++ {
		rb.Add(i)
	}
}

func BenchmarkRingBuffer_GetAll(b *testing.B) {
	rb := NewRingBuffer[int](1000)
	for i := 0; i < 1000; i++ {
		rb.Add(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.GetAll()
	}
}

func BenchmarkRingBuffer_Get(b *testing.B) {
	rb := NewRingBuffer[int](1000)
	for i := 0; i < 1000; i++ {
		rb.Add(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.Get(10)
	}
}

func BenchmarkRingBuffer_ConcurrentAdd(b *testing.B) {
	rb := NewRingBuffer[int](10000)
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			rb.Add(i)
			i++
		}
	})
}

func BenchmarkRingBuffer_ConcurrentAddAndGet(b *testing.B) {
	rb := NewRingBuffer[int](10000)
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%2 == 0 {
				rb.Add(i)
			} else {
				rb.Get(10)
			}
			i++
		}
	})
}
